package logging

import (
	"bytes"
	"encoding"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"sync"

	"google.golang.org/protobuf/proto"
)

const (
	// logTagKey is the struct tag that marks a field to leave out of the log,
	// `log:"redact"`, or to log masked, `log:"mask"`.
	logTagKey      = "log"
	redactTagValue = "redact"
	maskTagValue   = "mask"

	// maskVisibleChars characters are kept at each end of a masked value, if
	// it has at least maskMinLength characters. Shorter values are replaced
	// by maskShort, so neither the value nor its length is logged.
	maskVisibleChars = 2
	maskMinLength    = 8
	maskShort        = "********"

	// maxStructDepth stops pointer cycles from recursing forever.
	maxStructDepth = 32
)

var (
	jsonMarshalerType = reflect.TypeFor[json.Marshaler]()
	textMarshalerType = reflect.TypeFor[encoding.TextMarshaler]()
	protoMessageType  = reflect.TypeFor[proto.Message]()
)

// Struct logs a Go value as JSON, the way encoding/json would marshal it, but
// without the struct fields tagged `log:"redact"`. It's meant for the request
// and response types of REST/JSON APIs, and works on nested structs,
// pointers, slices, arrays and maps at any depth. Protobuf messages found
// inside the value are logged without their [debug_redact = true] fields,
// like with Proto. The value passed in is not modified.
//
// Field names, `json:"-"`, omitempty and omitzero follow the json tags, so the
// log matches what's sent over the wire. Values whose type implements
// json.Marshaler or encoding.TextMarshaler are logged with that method and
// aren't inspected. Neither are map[string]any or json.RawMessage values.
//
// The value is serialised when the first entry using the field is written,
// and only once. If nothing is logged, it is never serialised.
func Struct(
	key string,
	value any,
) slog.Attr {
	return slog.Any(key, lazyValue(sync.OnceValue(func() slog.Value {
		return redactedStructJSON(value)
	})))
}

func redactedStructJSON(value any) slog.Value {
	redacted, err := redactValue(reflect.ValueOf(value), 0)
	if err != nil {
		return slog.StringValue(fmt.Sprintf("<struct marshal error: %v>", err))
	}

	bytes, err := json.Marshal(redacted)
	if err != nil {
		return slog.StringValue(fmt.Sprintf("<struct marshal error: %v>", err))
	}
	return slog.AnyValue(json.RawMessage(bytes))
}

// redactValue returns a copy of v, built from maps, slices and scalars, that
// encoding/json marshals like v but without the redacted fields.
func redactValue(v reflect.Value, depth int) (any, error) {
	if !v.IsValid() {
		return nil, nil
	}
	if depth > maxStructDepth {
		return nil, fmt.Errorf("value nested deeper than %d levels, is it cyclic?", maxStructDepth)
	}

	if v.CanInterface() {
		// encoding/json also calls pointer-receiver methods on addressable values
		if v.Kind() != reflect.Pointer && v.CanAddr() {
			pt := reflect.PointerTo(v.Type())
			if pt.Implements(protoMessageType) {
				return redactedProtoJSON(v.Addr().Interface().(proto.Message))
			}
			if pt.Implements(jsonMarshalerType) || pt.Implements(textMarshalerType) {
				return v.Addr().Interface(), nil
			}
		}
		if v.Type().Implements(protoMessageType) {
			return redactedProtoJSON(v.Interface().(proto.Message))
		}
		if v.Type().Implements(jsonMarshalerType) || v.Type().Implements(textMarshalerType) {
			// encoding/json calls the method, we can't see what it writes
			return v.Interface(), nil
		}
	}

	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return nil, nil
		}
		return redactValue(v.Elem(), depth+1)

	case reflect.Struct:
		return structFields(v, depth)

	case reflect.Map:
		if v.IsNil() {
			return nil, nil
		}
		m := make(map[string]any, v.Len())
		iter := v.MapRange()
		for iter.Next() {
			key, err := mapKey(iter.Key())
			if err != nil {
				return nil, err
			}
			value, err := redactValue(iter.Value(), depth+1)
			if err != nil {
				return nil, err
			}
			m[key] = value
		}
		return m, nil

	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			return nil, nil
		}
		if v.Type().Elem().Kind() == reflect.Uint8 && v.CanInterface() {
			// []byte is logged as base64, like encoding/json does
			return v.Interface(), nil
		}
		list := make([]any, v.Len())
		for i := range v.Len() {
			value, err := redactValue(v.Index(i), depth+1)
			if err != nil {
				return nil, err
			}
			list[i] = value
		}
		return list, nil

	// Kind-specific getters also work on values reached through unexported
	// embedded structs, where Interface() would panic.
	case reflect.Bool:
		return v.Bool(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int(), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint(), nil
	case reflect.Float32, reflect.Float64:
		return v.Float(), nil
	case reflect.String:
		return v.String(), nil

	default:
		// Channels, functions and complex numbers can't be marshaled to JSON
		return nil, nil
	}
}

// structField is a field of a struct, or of a struct embedded in it, that
// encoding/json may marshal.
type structField struct {
	name   string
	depth  int  // 0 for the struct's own fields, +1 per embedded struct
	tagged bool // the name comes from a json tag
	log    string
	opts   []string
	value  reflect.Value // invalid if the field is in a nil embedded pointer
}

// structFields returns the fields of the struct v that encoding/json would
// marshal, in the same order, with redacted fields left out and masked
// fields masked.
func structFields(v reflect.Value, depth int) (orderedObject, error) {
	var candidates []structField
	if err := collectFields(&candidates, v.Type(), v, 0, "", depth); err != nil {
		return nil, err
	}

	fields := make(orderedObject, 0, len(candidates))
	for _, f := range dominantFields(candidates) {
		if f.log == redactTagValue || !f.value.IsValid() {
			continue
		}
		if slices.Contains(f.opts, "omitempty") && isEmptyValue(f.value) {
			continue
		}
		if slices.Contains(f.opts, "omitzero") && f.value.IsZero() {
			continue
		}

		var value any
		if f.log == maskTagValue {
			masked, ok := maskValue(f.value)
			if !ok {
				// Only strings can be masked, leave anything else out
				continue
			}
			value = masked
		} else {
			var err error
			if value, err = redactValue(f.value, depth+1); err != nil {
				return nil, err
			}
		}
		fields = append(fields, objectField{name: f.name, value: value})
	}
	return fields, nil
}

// collectFields appends the fields of the struct type t to fields, promoting
// the fields of embedded structs without a json name like encoding/json
// does. v is invalid when t is reached through a nil embedded pointer; its
// fields still take part in resolving name conflicts, as in encoding/json.
// A log tag on an embedded struct applies to all its promoted fields.
func collectFields(
	fields *[]structField,
	t reflect.Type,
	v reflect.Value,
	embedDepth int,
	inheritedLog string,
	depth int,
) error {
	if embedDepth > maxStructDepth {
		return fmt.Errorf("struct embedded deeper than %d levels, is it cyclic?", maxStructDepth)
	}

	for i := range t.NumField() {
		sf := t.Field(i)
		name, opts, _ := strings.Cut(sf.Tag.Get("json"), ",")
		if name == "-" && opts == "" {
			continue
		}

		log := sf.Tag.Get(logTagKey)
		if inheritedLog == redactTagValue || (inheritedLog == maskTagValue && log != redactTagValue) {
			log = inheritedLog
		}

		var fv reflect.Value
		if v.IsValid() {
			fv = v.Field(i)
		}

		if sf.Anonymous && name == "" {
			ft := sf.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
				if fv.IsValid() {
					if fv.IsNil() {
						fv = reflect.Value{}
					} else {
						fv = fv.Elem()
					}
				}
			}
			if ft.Kind() == reflect.Struct {
				if err := collectFields(fields, ft, fv, embedDepth+1, log, depth); err != nil {
					return err
				}
				continue
			}
		}
		if !sf.IsExported() {
			continue
		}

		tagged := name != ""
		if !tagged {
			name = sf.Name
		}
		*fields = append(*fields, structField{
			name:   name,
			depth:  embedDepth,
			tagged: tagged,
			log:    log,
			opts:   strings.Split(opts, ","),
			value:  fv,
		})
	}
	return nil
}

// dominantFields resolves fields with the same name like encoding/json: the
// least embedded field wins, then the one named by a json tag. If that still
// leaves more than one, none of them is marshaled. Redacted fields take part
// too, so a redacted field can't let a field it hides be logged.
func dominantFields(fields []structField) []structField {
	byName := make(map[string][]int, len(fields))
	for i, f := range fields {
		byName[f.name] = append(byName[f.name], i)
	}

	keep := make([]bool, len(fields))
	for _, indexes := range byName {
		best := indexes[0]
		ambiguous := false
		for _, i := range indexes[1:] {
			a, b := fields[i], fields[best]
			switch {
			case a.depth < b.depth || (a.depth == b.depth && a.tagged && !b.tagged):
				best, ambiguous = i, false
			case a.depth == b.depth && a.tagged == b.tagged:
				ambiguous = true
			}
		}
		if !ambiguous {
			keep[best] = true
		}
	}

	dominant := make([]structField, 0, len(fields))
	for i, f := range fields {
		if keep[i] {
			dominant = append(dominant, f)
		}
	}
	return dominant
}

// maskValue masks a string, or a pointer to one: the first and last
// maskVisibleChars characters are kept and the rest replaced with '*'.
// Values shorter than maskMinLength characters are replaced by maskShort.
// It reports false if v isn't a string.
func maskValue(v reflect.Value) (any, bool) {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil, true
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.String {
		return nil, false
	}

	runes := []rune(v.String())
	switch {
	case len(runes) == 0:
		return "", true
	case len(runes) < maskMinLength:
		return maskShort, true
	}
	hidden := len(runes) - 2*maskVisibleChars
	return string(runes[:maskVisibleChars]) + strings.Repeat("*", hidden) + string(runes[len(runes)-maskVisibleChars:]), true
}

func mapKey(k reflect.Value) (string, error) {
	if k.Kind() == reflect.String {
		return k.String(), nil
	}
	if k.CanInterface() && k.Type().Implements(textMarshalerType) {
		text, err := k.Interface().(encoding.TextMarshaler).MarshalText()
		return string(text), err
	}
	switch k.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fmt.Sprint(k.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return fmt.Sprint(k.Uint()), nil
	}
	return "", fmt.Errorf("unsupported map key type %s", k.Type())
}

// isEmptyValue reports whether omitempty leaves v out, like encoding/json.
func isEmptyValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64,
		reflect.Interface, reflect.Pointer:
		return v.IsZero()
	}
	return false
}

// orderedObject is a JSON object that keeps the struct's field order.
type orderedObject []objectField

type objectField struct {
	name  string
	value any
}

func (o orderedObject) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, f := range o {
		if i > 0 {
			buf.WriteByte(',')
		}
		name, err := json.Marshal(f.name)
		if err != nil {
			return nil, err
		}
		value, err := json.Marshal(f.value)
		if err != nil {
			return nil, err
		}
		buf.Write(name)
		buf.WriteByte(':')
		buf.Write(value)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}
