package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// mockHandler implements slog.Handler and records log entries for assertions.
type mockHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *mockHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *mockHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}
func (h *mockHandler) WithAttrs(attrs []slog.Attr) slog.Handler { return h }
func (h *mockHandler) WithGroup(name string) slog.Handler       { return h }

func (h *mockHandler) LastMessage() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.records) == 0 {
		return ""
	}
	return h.records[len(h.records)-1].Message
}

func TestLoggingTransport_RoundTrip(t *testing.T) {
	// Mock RoundTripper that returns a fixed response
	mockRT := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(bytes.NewBufferString("ok")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})

	mh := &mockHandler{}
	logger := slog.New(mh)
	lt := NewLoggingTransport(
		mockRT,
		&Logger{
			Logger: logger,
		},
		&LoggingOptions{
			DumpRequestFunc:  DumpRequest,
			DumpResponseFunc: DumpResponse,
		},
	)

	req, err := http.NewRequest("GET", "http://example.com", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := lt.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
	lastMsg := mh.LastMessage()
	if lastMsg != "called external service" {
		t.Errorf("expected log message, got %q", lastMsg)
	}
}

func TestLoggingTransport_DumpFieldsDoNotCollideWithIncomingRequest(t *testing.T) {
	mockRT := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			ProtoMajor: 1,
			ProtoMinor: 1,
			Body:       io.NopCloser(bytes.NewBufferString("ok")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})

	var buf bytes.Buffer
	lt := NewLoggingTransport(
		mockRT,
		NewLogger(&LoggerConfig{MinLevel: DebugLevel, Output: &buf}),
		&LoggingOptions{
			DumpRequestFunc:  DumpRequest,
			DumpResponseFunc: DumpResponse,
		},
	)

	// Simulates a middleware that logs the incoming request with every entry.
	ctx := ContextWithFields(context.Background(), String("request", "incoming"))
	req, err := http.NewRequestWithContext(ctx, "GET", "http://example.com/out", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	if _, err := lt.RoundTrip(req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var logMap map[string]any
	if err := json.Unmarshal(buf.Bytes(), &logMap); err != nil {
		t.Fatalf("failed to parse log entry %q: %v", buf.String(), err)
	}
	if v := logMap["request"]; v != "incoming" {
		t.Errorf("expected request='incoming', got: %v", v)
	}
	if v, _ := logMap["outgoing_request"].(string); !strings.HasPrefix(v, "GET /out HTTP/1.1") {
		t.Errorf("expected outgoing_request dump, got: %q", v)
	}
	if v, _ := logMap["outgoing_response"].(string); !strings.HasPrefix(v, "HTTP/1.1 200 OK") {
		t.Errorf("expected outgoing_response dump, got: %q", v)
	}
	if _, ok := logMap["response"]; ok {
		t.Errorf("unexpected response field: %v", logMap["response"])
	}
}

// roundTripperFunc allows using a function as an http.RoundTripper.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
