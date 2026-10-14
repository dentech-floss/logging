package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dentech-floss/logging/pkg/logging"
)

type restAddress struct {
	Street string `json:"street" log:"redact"`
	City   string `json:"city"`
}

type restMeta struct {
	RequestID string `json:"request_id"`
	APIKey    string `json:"api_key" log:"redact"`
}

type restPatient struct {
	restMeta

	ID             string                  `json:"id"`
	Name           string                  `json:"name" log:"redact"`
	Address        *restAddress            `json:"address,omitempty"`
	Previous       []restAddress           `json:"previous_addresses"`
	ByType         map[string]*restAddress `json:"addresses_by_type"`
	SecretAddress  restAddress             `json:"secret_address" log:"redact"`
	Notes          string                  `json:"notes,omitempty"`
	BornAt         time.Time               `json:"born_at"`
	Internal       string                  `json:"-"`
	NoTag          int
	unexported     string
	RedactedNoJSON string `log:"redact"`
}

func newRestPatient() *restPatient {
	return &restPatient{
		restMeta:       restMeta{RequestID: "req-1", APIKey: "secret-key"},
		ID:             "42",
		Name:           "Anna Andersson",
		Address:        &restAddress{Street: "Storgatan 1", City: "Stockholm"},
		Previous:       []restAddress{{Street: "Lillgatan 3", City: "Göteborg"}},
		ByType:         map[string]*restAddress{"home": {Street: "Hemgatan 4", City: "Malmö"}},
		SecretAddress:  restAddress{Street: "Hemligvägen 2", City: "Uppsala"},
		BornAt:         time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC),
		Internal:       "internal",
		NoTag:          7,
		unexported:     "unexported",
		RedactedNoJSON: "redacted",
	}
}

func TestStructRedactsTaggedFields(t *testing.T) {
	attr := logging.Struct("patient", newRestPatient())
	if attr.Key != "patient" {
		t.Errorf("Expected key patient, got: %s", attr.Key)
	}

	got := attrJSON(t, attr.Value.Resolve().Any())

	for _, key := range []string{"name", "secret_address", "api_key", "RedactedNoJSON"} {
		if v, ok := got[key]; ok {
			t.Errorf("Expected %s to be redacted, got: %v", key, v)
		}
	}
	if v := got["id"]; v != "42" {
		t.Errorf("Expected id=42, got: %v", v)
	}
	if v := got["request_id"]; v != "req-1" {
		t.Errorf("Expected embedded request_id=req-1, got: %v", v)
	}

	address := got["address"].(map[string]any)
	if v, ok := address["street"]; ok {
		t.Errorf("Expected address.street behind a pointer to be redacted, got: %v", v)
	}
	if v := address["city"]; v != "Stockholm" {
		t.Errorf("Expected address.city=Stockholm, got: %v", v)
	}

	previous := got["previous_addresses"].([]any)[0].(map[string]any)
	if v, ok := previous["street"]; ok {
		t.Errorf("Expected street in slice to be redacted, got: %v", v)
	}
	if v := previous["city"]; v != "Göteborg" {
		t.Errorf("Expected city in slice=Göteborg, got: %v", v)
	}

	home := got["addresses_by_type"].(map[string]any)["home"].(map[string]any)
	if v, ok := home["street"]; ok {
		t.Errorf("Expected street in map to be redacted, got: %v", v)
	}
	if v := home["city"]; v != "Malmö" {
		t.Errorf("Expected city in map=Malmö, got: %v", v)
	}
}

func TestStructFollowsJSONTags(t *testing.T) {
	got := attrJSON(t, logging.Struct("patient", newRestPatient()).Value.Resolve().Any())

	if v, ok := got["notes"]; ok {
		t.Errorf("Expected empty notes to be left out by omitempty, got: %v", v)
	}
	if v, ok := got["Internal"]; ok {
		t.Errorf(`Expected json:"-" field to be left out, got: %v`, v)
	}
	if v, ok := got["unexported"]; ok {
		t.Errorf("Expected unexported field to be left out, got: %v", v)
	}
	if v := got["NoTag"]; v != float64(7) {
		t.Errorf("Expected NoTag=7 under its Go name, got: %v", v)
	}
	if v := got["born_at"]; v != "1990-01-01T00:00:00Z" {
		t.Errorf("Expected born_at to use time.Time's MarshalJSON, got: %v", v)
	}
}

func TestStructKeepsFieldOrder(t *testing.T) {
	type ordered struct {
		Zebra string `json:"zebra"`
		Apple string `json:"apple"`
	}

	raw := logging.Struct("v", ordered{Zebra: "z", Apple: "a"}).Value.Resolve().Any().(json.RawMessage)
	if want := `{"zebra":"z","apple":"a"}`; string(raw) != want {
		t.Errorf("Expected %s, got: %s", want, raw)
	}
}

func TestStructRedactsNestedProto(t *testing.T) {
	type envelope struct {
		Patient any `json:"patient"`
	}

	got := attrJSON(t, logging.Struct("v", envelope{Patient: newPatient(t)}).Value.Resolve().Any())

	patient := got["patient"].(map[string]any)
	if v, ok := patient["name"]; ok {
		t.Errorf("Expected debug_redact field in nested proto to be redacted, got: %v", v)
	}
	if v := patient["id"]; v != "42" {
		t.Errorf("Expected id=42, got: %v", v)
	}
}

func TestStructDoesNotModifyValue(t *testing.T) {
	patient := newRestPatient()
	_ = logging.Struct("patient", patient).Value.Resolve()

	if patient.Name != "Anna Andersson" || patient.Address.Street != "Storgatan 1" {
		t.Error("Expected the original value to be unchanged")
	}
}

func TestStructNilAndScalars(t *testing.T) {
	var typedNil *restPatient

	tests := []struct {
		name  string
		value any
		want  string
	}{
		{name: "nil interface", value: nil, want: "null"},
		{name: "typed nil", value: typedNil, want: "null"},
		{name: "string", value: "plain", want: `"plain"`},
		{name: "slice of structs", value: []restAddress{{Street: "s", City: "c"}}, want: `[{"city":"c"}]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := logging.Struct("v", tt.value).Value.Resolve().Any()
			if raw, ok := got.(json.RawMessage); !ok || string(raw) != tt.want {
				t.Errorf("Expected %s, got: %v", tt.want, got)
			}
		})
	}
}

func TestStructCyclicValue(t *testing.T) {
	type node struct {
		Next *node `json:"next"`
	}
	n := &node{}
	n.Next = n

	got := logging.Struct("v", n).Value.Resolve().Any()
	if s, ok := got.(string); !ok || !strings.HasPrefix(s, "<struct marshal error:") {
		t.Errorf("Expected a marshal error text, got: %v", got)
	}
}

func TestStructIsSerialisedOnceWhenLogged(t *testing.T) {
	var buf bytes.Buffer
	logger := newBufferLogger(&buf)

	address := &restAddress{Street: "Storgatan 1", City: "before"}
	ctx := logging.ContextWithFields(context.Background(), logging.Struct("address", address))

	// Not serialised yet, so the logged value is the struct as it is when first logged
	address.City = "first log"
	logger.InfoContext(ctx, "first")

	// Serialised once: later entries reuse the first result
	address.City = "second log"
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
		logged := logMap["address"].(map[string]any)
		if v := logged["city"]; v != "first log" {
			t.Errorf("Line %d: expected city=first log, got: %v", i+1, v)
		}
		if v, ok := logged["street"]; ok {
			t.Errorf("Line %d: expected street to be redacted, got: %v", i+1, v)
		}
	}
}

func TestStructMasksTaggedStrings(t *testing.T) {
	type credentials struct {
		Token     string  `json:"token" log:"mask"`
		Short     string  `json:"short" log:"mask"`
		Empty     string  `json:"empty" log:"mask"`
		Unicode   string  `json:"unicode" log:"mask"`
		Pointer   *string `json:"pointer" log:"mask"`
		NilPtr    *string `json:"nil_pointer" log:"mask"`
		Omitted   string  `json:"omitted,omitempty" log:"mask"`
		NotString int     `json:"not_string" log:"mask"`
	}
	pointer := "pointer-token-1234"

	got := attrJSON(t, logging.Struct("v", credentials{
		Token:     "sk_live_abcdef123456",
		Short:     "1234567",
		Unicode:   "ÅÄÖåäöÉé",
		Pointer:   &pointer,
		NotString: 42,
	}).Value.Resolve().Any())

	want := map[string]any{
		"token":       "sk****************56",
		"short":       "********",
		"empty":       "",
		"unicode":     "ÅÄ****Éé",
		"pointer":     "po**************34",
		"nil_pointer": nil,
	}
	for key, w := range want {
		if v, ok := got[key]; !ok || v != w {
			t.Errorf("Expected %s=%v, got: %v", key, w, v)
		}
	}
	for _, key := range []string{"omitted", "not_string"} {
		if v, ok := got[key]; ok {
			t.Errorf("Expected %s to be left out, got: %v", key, v)
		}
	}
}

func TestStructMasksEmbeddedStruct(t *testing.T) {
	type keys struct {
		APIKey string `json:"api_key"`
		Secret string `json:"secret" log:"redact"`
	}
	type request struct {
		keys `log:"mask"`
		ID   string `json:"id"`
	}

	got := attrJSON(t, logging.Struct("v", request{
		keys: keys{APIKey: "key-1234567890", Secret: "secret"},
		ID:   "1",
	}).Value.Resolve().Any())

	if v := got["api_key"]; v != "ke**********90" {
		t.Errorf("Expected promoted api_key to be masked, got: %v", v)
	}
	if v, ok := got["secret"]; ok {
		t.Errorf("Expected redact to win over an inherited mask, got: %v", v)
	}
}

// pointerSecret masks itself on the wire with a pointer-receiver MarshalJSON.
type pointerSecret struct {
	Value string
}

func (s *pointerSecret) MarshalJSON() ([]byte, error) {
	return []byte(`"***"`), nil
}

func TestStructUsesPointerReceiverMarshaler(t *testing.T) {
	type request struct {
		Secret pointerSecret `json:"secret"`
	}
	req := &request{Secret: pointerSecret{Value: "raw-secret"}}

	wire, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	logged := logging.Struct("v", req).Value.Resolve().Any().(json.RawMessage)

	if string(logged) != string(wire) {
		t.Errorf("Expected %s like encoding/json, got: %s", wire, logged)
	}
}

func TestStructResolvesFieldNameConflictsLikeJSON(t *testing.T) {
	type inner struct {
		Shadowed string `json:"name"`
		Deeper   string `json:"deeper"`
	}
	type a struct {
		Ambiguous string
	}
	type b struct {
		Ambiguous string
	}
	type tagWins1 struct {
		TagWins string `json:"tag_wins"`
	}
	type tagWins2 struct {
		TagWins string
	}
	type outer struct {
		inner
		a
		b
		tagWins1
		tagWins2
		Name   string `json:"name"`
		Deeper string `json:"deeper" log:"redact"`
	}
	value := outer{
		inner:    inner{Shadowed: "embedded-secret", Deeper: "embedded-secret"},
		a:        a{Ambiguous: "a"},
		b:        b{Ambiguous: "b"},
		tagWins1: tagWins1{TagWins: "tagged"},
		tagWins2: tagWins2{TagWins: "untagged"},
		Name:     "outer",
		Deeper:   "outer-secret",
	}

	got := attrJSON(t, logging.Struct("v", value).Value.Resolve().Any())

	if v := got["name"]; v != "outer" {
		t.Errorf("Expected the outer name to win, got: %v", v)
	}
	if v, ok := got["deeper"]; ok {
		t.Errorf("Expected a redacted outer field to hide the embedded one, got: %v", v)
	}
	if v, ok := got["Ambiguous"]; ok {
		t.Errorf("Expected ambiguous fields to be left out, got: %v", v)
	}
	if v := got["tag_wins"]; v != "tagged" {
		t.Errorf("Expected the json-tagged field to win, got: %v", v)
	}

	// Without redaction, the keys match what encoding/json marshals
	type plain struct {
		inner
		a
		b
		Name string `json:"name"`
	}
	p := plain{inner: inner{Shadowed: "x", Deeper: "y"}, a: a{"a"}, b: b{"b"}, Name: "outer"}
	wire, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if logged := logging.Struct("v", p).Value.Resolve().Any().(json.RawMessage); string(logged) != string(wire) {
		t.Errorf("Expected %s like encoding/json, got: %s", wire, logged)
	}
}
