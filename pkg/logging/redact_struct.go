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
	// redactTagKey and redactTagValue mark a struct field to leave out of the
	// log: `log:"redact"`.
	redactTagKey   = "log"
	redactTagValue = "redact"

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
		fields := make(orderedObject, 0, v.NumField())
		if err := appendStructFields(&fields, v, depth); err != nil {
			return nil, err
		}
		return fields, nil

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

// appendStructFields appends the fields of the struct v to fields. Fields of
// embedded structs without a json name are promoted, like encoding/json does.
func appendStructFields(fields *orderedObject, v reflect.Value, depth int) error {
	t := v.Type()
	for i := range t.NumField() {
		sf := t.Field(i)
		if sf.Tag.Get(redactTagKey) == redactTagValue {
			continue
		}

		name, opts, _ := strings.Cut(sf.Tag.Get("json"), ",")
		if name == "-" && opts == "" {
			continue
		}

		fv := v.Field(i)
		if sf.Anonymous && name == "" {
			ft := sf.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				if fv.Kind() == reflect.Pointer {
					if fv.IsNil() {
						continue
					}
					fv = fv.Elem()
				}
				if err := appendStructFields(fields, fv, depth+1); err != nil {
					return err
				}
				continue
			}
		}
		if !sf.IsExported() {
			continue
		}

		optList := strings.Split(opts, ",")
		if slices.Contains(optList, "omitempty") && isEmptyValue(fv) {
			continue
		}
		if slices.Contains(optList, "omitzero") && fv.IsZero() {
			continue
		}

		if name == "" {
			name = sf.Name
		}
		value, err := redactValue(fv, depth+1)
		if err != nil {
			return err
		}
		*fields = append(*fields, objectField{name: name, value: value})
	}
	return nil
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
