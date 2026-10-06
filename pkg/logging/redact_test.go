package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/dentech-floss/logging/pkg/logging"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// patientDescriptor builds the equivalent of:
//
//	message Address {
//	  string street = 1 [debug_redact = true];
//	  string city = 2;
//	}
//
//	message Patient {
//	  string id = 1;
//	  string name = 2 [debug_redact = true];
//	  Address address = 3;
//	  repeated Address previous_addresses = 4;
//	  map<string, Address> addresses_by_type = 5;
//	  Address secret_address = 6 [debug_redact = true];
//	}
func patientDescriptor(t *testing.T) protoreflect.MessageDescriptor {
	t.Helper()

	redacted := &descriptorpb.FieldOptions{DebugRedact: proto.Bool(true)}
	field := func(
		name string,
		number int32,
		typ descriptorpb.FieldDescriptorProto_Type,
		typeName string,
		label descriptorpb.FieldDescriptorProto_Label,
		opts *descriptorpb.FieldOptions,
	) *descriptorpb.FieldDescriptorProto {
		f := &descriptorpb.FieldDescriptorProto{
			Name:    proto.String(name),
			Number:  proto.Int32(number),
			Type:    typ.Enum(),
			Label:   label.Enum(),
			Options: opts,
		}
		if typeName != "" {
			f.TypeName = proto.String(typeName)
		}
		return f
	}

	const (
		str      = descriptorpb.FieldDescriptorProto_TYPE_STRING
		msg      = descriptorpb.FieldDescriptorProto_TYPE_MESSAGE
		optional = descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
		repeated = descriptorpb.FieldDescriptorProto_LABEL_REPEATED
	)

	file := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("test/redact.proto"),
		Package: proto.String("test"),
		Syntax:  proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("Address"),
				Field: []*descriptorpb.FieldDescriptorProto{
					field("street", 1, str, "", optional, redacted),
					field("city", 2, str, "", optional, nil),
				},
			},
			{
				Name: proto.String("Patient"),
				Field: []*descriptorpb.FieldDescriptorProto{
					field("id", 1, str, "", optional, nil),
					field("name", 2, str, "", optional, redacted),
					field("address", 3, msg, ".test.Address", optional, nil),
					field("previous_addresses", 4, msg, ".test.Address", repeated, nil),
					field("addresses_by_type", 5, msg, ".test.Patient.AddressesByTypeEntry", repeated, nil),
					field("secret_address", 6, msg, ".test.Address", optional, redacted),
				},
				NestedType: []*descriptorpb.DescriptorProto{
					{
						Name: proto.String("AddressesByTypeEntry"),
						Field: []*descriptorpb.FieldDescriptorProto{
							field("key", 1, str, "", optional, nil),
							field("value", 2, msg, ".test.Address", optional, nil),
						},
						Options: &descriptorpb.MessageOptions{MapEntry: proto.Bool(true)},
					},
				},
			},
		},
	}

	fd, err := protodesc.NewFile(file, nil)
	if err != nil {
		t.Fatalf("Failed to build test descriptor: %v", err)
	}
	return fd.Messages().ByName("Patient")
}

func newPatient(t *testing.T) *dynamicpb.Message {
	t.Helper()

	md := patientDescriptor(t)
	fields := md.Fields()
	addressMd := fields.ByName("address").Message()

	newAddress := func(street, city string) protoreflect.Value {
		a := dynamicpb.NewMessage(addressMd)
		a.Set(addressMd.Fields().ByName("street"), protoreflect.ValueOfString(street))
		a.Set(addressMd.Fields().ByName("city"), protoreflect.ValueOfString(city))
		return protoreflect.ValueOfMessage(a)
	}

	p := dynamicpb.NewMessage(md)
	p.Set(fields.ByName("id"), protoreflect.ValueOfString("42"))
	p.Set(fields.ByName("name"), protoreflect.ValueOfString("Anna Andersson"))
	p.Set(fields.ByName("address"), newAddress("Storgatan 1", "Stockholm"))
	p.Set(fields.ByName("secret_address"), newAddress("Hemligvägen 2", "Uppsala"))

	previous := p.Mutable(fields.ByName("previous_addresses")).List()
	previous.Append(newAddress("Lillgatan 3", "Göteborg"))

	byType := p.Mutable(fields.ByName("addresses_by_type")).Map()
	byType.Set(protoreflect.ValueOfString("home").MapKey(), newAddress("Hemgatan 4", "Malmö"))

	return p
}

func attrJSON(t *testing.T, v any) map[string]any {
	t.Helper()

	raw, ok := v.(json.RawMessage)
	if !ok {
		t.Fatalf("Expected json.RawMessage, got: %T", v)
	}

	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}
	return m
}

func TestProtoRedactsMarkedFields(t *testing.T) {
	patient := newPatient(t)

	attr := logging.Proto("patient", patient)

	if attr.Key != "patient" {
		t.Errorf("Expected key patient, got: %s", attr.Key)
	}

	got := attrJSON(t, attr.Value.Resolve().Any())

	if v := got["id"]; v != "42" {
		t.Errorf("Expected id=42, got: %v", v)
	}
	if v, ok := got["name"]; ok {
		t.Errorf("Expected name to be redacted, got: %v", v)
	}
	if v, ok := got["secretAddress"]; ok {
		t.Errorf("Expected secretAddress to be redacted, got: %v", v)
	}

	address := got["address"].(map[string]any)
	if v, ok := address["street"]; ok {
		t.Errorf("Expected nested address.street to be redacted, got: %v", v)
	}
	if v := address["city"]; v != "Stockholm" {
		t.Errorf("Expected address.city=Stockholm, got: %v", v)
	}

	previous := got["previousAddresses"].([]any)[0].(map[string]any)
	if v, ok := previous["street"]; ok {
		t.Errorf("Expected street in list to be redacted, got: %v", v)
	}
	if v := previous["city"]; v != "Göteborg" {
		t.Errorf("Expected city in list=Göteborg, got: %v", v)
	}

	home := got["addressesByType"].(map[string]any)["home"].(map[string]any)
	if v, ok := home["street"]; ok {
		t.Errorf("Expected street in map to be redacted, got: %v", v)
	}
	if v := home["city"]; v != "Malmö" {
		t.Errorf("Expected city in map=Malmö, got: %v", v)
	}
}

func TestProtoDoesNotModifyMessage(t *testing.T) {
	patient := newPatient(t)
	before := proto.Clone(patient)

	_ = logging.Proto("patient", patient).Value.Resolve()

	if !proto.Equal(before, patient) {
		t.Error("Expected the original message to be unchanged")
	}
}

func TestProtoNil(t *testing.T) {
	var typedNil *wrapperspb.StringValue

	tests := []struct {
		name  string
		value proto.Message
	}{
		{name: "nil interface", value: nil},
		{name: "typed nil", value: typedNil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want, err := protojson.Marshal(tt.value)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			got := logging.Proto("request", tt.value).Value.Resolve().Any()

			if raw, ok := got.(json.RawMessage); !ok || string(raw) != string(want) {
				t.Errorf("Expected %s, got: %v", want, got)
			}
		})
	}
}

func TestProtoIsSerialisedOnceWhenLogged(t *testing.T) {
	var buf bytes.Buffer
	logger := newBufferLogger(&buf)

	msg := wrapperspb.String("before")
	ctx := logging.ContextWithFields(context.Background(), logging.Proto("request", msg))

	// Not serialised yet, so the logged value is the message as it is when first logged
	msg.Value = "first log"
	logger.InfoContext(ctx, "first")

	// Serialised once: later entries reuse the first result
	msg.Value = "second log"
	logger.InfoContext(ctx, "second")

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	if len(lines) != 2 {
		t.Fatalf("Expected 2 log lines, got: %d", len(lines))
	}

	for i, line := range lines {
		var logMap map[string]any
		if err := json.Unmarshal(line, &logMap); err != nil {
			t.Fatalf("Failed to parse JSON log: %v", err)
		}
		if v := logMap["request"]; v != "first log" {
			t.Errorf("Line %d: expected request=first log, got: %v", i+1, v)
		}
	}
}
