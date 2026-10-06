package grpclogging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/dentech-floss/logging/pkg/logging"
	"github.com/dentech-floss/logging/pkg/logging/grpclogging"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const fullMethod = "/test.v1.TestService/DoSomething"

func runInterceptor(t *testing.T, req any, opts ...grpclogging.Option) map[string]any {
	t.Helper()

	var buf bytes.Buffer
	logger := logging.NewLogger(&logging.LoggerConfig{
		ServiceName: "test-service",
		MinLevel:    logging.DebugLevel,

		Output: &buf,
	})

	handler := func(ctx context.Context, req any) (any, error) {
		logger.InfoContext(ctx, "Handled")
		return "response", nil
	}

	resp, err := grpclogging.UnaryServerInterceptor(opts...)(
		context.Background(),
		req,
		&grpc.UnaryServerInfo{FullMethod: fullMethod},
		handler,
	)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if resp != "response" {
		t.Errorf("Expected the handler's response, got: %v", resp)
	}

	var logMap map[string]any
	if err := json.Unmarshal(buf.Bytes(), &logMap); err != nil {
		t.Fatalf("Failed to parse JSON log: %v", err)
	}
	return logMap
}

func TestUnaryServerInterceptor(t *testing.T) {
	logMap := runInterceptor(t, wrapperspb.String("hello"))

	if v := logMap["grpc.method"]; v != fullMethod {
		t.Errorf("Expected grpc.method=%s, got: %v", fullMethod, v)
	}
	if v, ok := logMap["request"]; ok {
		t.Errorf("Did not expect the request to be logged by default, got: %v", v)
	}
}

func TestUnaryServerInterceptorWithRequest(t *testing.T) {
	logMap := runInterceptor(t, wrapperspb.String("hello"), grpclogging.WithRequest())

	if v := logMap["grpc.method"]; v != fullMethod {
		t.Errorf("Expected grpc.method=%s, got: %v", fullMethod, v)
	}
	if v := logMap["request"]; v != "hello" {
		t.Errorf("Expected the request to be logged, got: %v", v)
	}
}

func TestUnaryServerInterceptorWithRedactedRequest(t *testing.T) {
	// Redaction itself is tested with logging.RedactedProto. This checks the wiring.
	logMap := runInterceptor(t, wrapperspb.String("hello"), grpclogging.WithRedactedRequest())

	if v := logMap["grpc.method"]; v != fullMethod {
		t.Errorf("Expected grpc.method=%s, got: %v", fullMethod, v)
	}
	if v := logMap["request"]; v != "hello" {
		t.Errorf("Expected the request to be logged, got: %v", v)
	}
}

func TestUnaryServerInterceptorWithRequestNonProtoRequest(t *testing.T) {
	logMap := runInterceptor(t, "not a proto message", grpclogging.WithRequest())

	if v := logMap["grpc.method"]; v != fullMethod {
		t.Errorf("Expected grpc.method=%s, got: %v", fullMethod, v)
	}
	if v, ok := logMap["request"]; ok {
		t.Errorf("Did not expect a request field, got: %v", v)
	}
}

func TestUnaryServerInterceptorWithRequestFilter(t *testing.T) {
	tests := []struct {
		name   string
		filter grpclogging.RequestFilter
		want   any
	}{
		{
			name: "skip",
			filter: func(_ *grpc.UnaryServerInfo, _ proto.Message) (slog.Attr, bool) {
				return slog.Attr{}, false
			},
			want: nil,
		},
		{
			name: "redact",
			filter: func(info *grpc.UnaryServerInfo, _ proto.Message) (slog.Attr, bool) {
				if info.FullMethod != fullMethod {
					t.Errorf("Expected the filter to get the call info, got: %v", info.FullMethod)
				}
				return logging.String("request", "[redacted]"), true
			},
			want: "[redacted]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logMap := runInterceptor(t, wrapperspb.String("secret"), grpclogging.WithRequestFilter(tt.filter))

			if v := logMap["grpc.method"]; v != fullMethod {
				t.Errorf("Expected grpc.method=%s, got: %v", fullMethod, v)
			}
			if v := logMap["request"]; v != tt.want {
				t.Errorf("Expected request=%v, got: %v", tt.want, v)
			}
		})
	}
}
