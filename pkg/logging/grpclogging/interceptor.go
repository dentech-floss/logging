// Package grpclogging adds request log fields to gRPC server calls.
package grpclogging

import (
	"context"

	"github.com/dentech-floss/logging/pkg/logging"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

// UnaryServerInterceptor adds the "grpc.method" and "request" fields to the
// context of every unary call, using logging.WithFields. Everything the handler
// logs with that context, through a *Context method, includes them.
//
// Register it with grpc.ChainUnaryInterceptor(grpclogging.UnaryServerInterceptor()).
func UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		ctx = logging.WithFields(ctx, logging.String("grpc.method", info.FullMethod))
		if msg, ok := req.(proto.Message); ok {
			ctx = logging.WithFields(ctx, logging.Proto("request", msg))
		}

		return handler(ctx, req)
	}
}
