package logging

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
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

// roundTripperFunc allows using a function as an http.RoundTripper.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
