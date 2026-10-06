package grpclogging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/dentech-floss/logging/pkg/logging"
	"github.com/dentech-floss/logging/pkg/logging/grpclogging"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const fullMethod = "/test.v1.TestService/DoSomething"

func runInterceptor(t *testing.T, req any) map[string]any {
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

	resp, err := grpclogging.UnaryServerInterceptor()(
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
	if v := logMap["request"]; v != "hello" {
		t.Errorf("Expected the request to be logged, got: %v", v)
	}
}

func TestUnaryServerInterceptorNonProtoRequest(t *testing.T) {
	logMap := runInterceptor(t, "not a proto message")

	if v := logMap["grpc.method"]; v != fullMethod {
		t.Errorf("Expected grpc.method=%s, got: %v", fullMethod, v)
	}
	if v, ok := logMap["request"]; ok {
		t.Errorf("Did not expect a request field, got: %v", v)
	}
}
