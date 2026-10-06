package logging

import (
	"log/slog"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

// RedactedProto works like Proto, but leaves out every field marked with
// [debug_redact = true] in the .proto schema, also in nested messages, lists
// and maps. The message passed in is not modified.
//
// Fields inside google.protobuf.Any values are not inspected.
func RedactedProto(
	key string,
	value proto.Message,
) slog.Attr {
	if value == nil {
		return Proto(key, value)
	}

	redacted := proto.Clone(value)
	redact(redacted.ProtoReflect())

	return Proto(key, redacted)
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
