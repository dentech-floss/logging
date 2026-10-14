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
