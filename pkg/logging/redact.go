package logging

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

// redactedJSON serialises m to JSON without the fields marked with
// [debug_redact = true]. Fields inside google.protobuf.Any values are not
// inspected.
func redactedJSON(m proto.Message) slog.Value {
	if m != nil && m.ProtoReflect().IsValid() {
		redacted := proto.Clone(m)
		redact(redacted.ProtoReflect())
		m = redacted
	}

	bytes, err := protojson.Marshal(m)
	if err != nil {
		return slog.StringValue(fmt.Sprintf("<proto marshal error: %v>", err))
	}
	return slog.AnyValue(json.RawMessage(bytes))
}

func redact(m protoreflect.Message) {
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		switch {
		case isRedacted(fd):
			m.Clear(fd)
		case fd.IsMap():
			if fd.MapValue().Message() != nil {
				v.Map().Range(func(_ protoreflect.MapKey, mv protoreflect.Value) bool {
					redact(mv.Message())
					return true
				})
			}
		case fd.Message() == nil:
			// Scalar field, nothing nested to redact
		case fd.IsList():
			list := v.List()
			for i := range list.Len() {
				redact(list.Get(i).Message())
			}
		default:
			redact(v.Message())
		}
		return true
	})
}

func isRedacted(fd protoreflect.FieldDescriptor) bool {
	opts, ok := fd.Options().(*descriptorpb.FieldOptions)
	return ok && opts.GetDebugRedact()
}
