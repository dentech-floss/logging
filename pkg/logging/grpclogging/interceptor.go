// Package grpclogging adds request log fields to gRPC server calls.
package grpclogging

import (
	"context"
	"log/slog"

	"github.com/dentech-floss/logging/pkg/logging"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

// RequestFilter decides how a request is logged. It returns the field to add
// and whether to add it, so it can skip a request or log a redacted version.
type RequestFilter func(info *grpc.UnaryServerInfo, req proto.Message) (slog.Attr, bool)

// Option configures UnaryServerInterceptor.
type Option func(*options)

type options struct {
	requestFilter RequestFilter
}

// WithRequest adds the full request as the "request" field on every call.
// Don't use it if requests can contain sensitive data; use WithRequestFilter.
func WithRequest() Option {
	return WithRequestFilter(func(_ *grpc.UnaryServerInfo, req proto.Message) (slog.Attr, bool) {
		return logging.Proto("request", req), true
	})
}

// WithRequestFilter adds the request field returned by filter, when filter
// returns true. Use it to skip requests of some methods or redact fields.
func WithRequestFilter(filter RequestFilter) Option {
	return func(o *options) {
		o.requestFilter = filter
	}
}

// UnaryServerInterceptor adds the "grpc.method" field to the context of every
// unary call, using logging.WithFields. Everything the handler logs with that
// context, through a *Context method, includes it.
//
// The request is not logged unless WithRequest or WithRequestFilter is passed.
//
// Register it with grpc.ChainUnaryInterceptor(grpclogging.UnaryServerInterceptor()).
func UnaryServerInterceptor(opts ...Option) grpc.UnaryServerInterceptor {
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}

	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		ctx = logging.WithFields(ctx, logging.String("grpc.method", info.FullMethod))

		if msg, ok := req.(proto.Message); ok && o.requestFilter != nil {
			if attr, ok := o.requestFilter(info, msg); ok {
				ctx = logging.WithFields(ctx, attr)
			}
		}

		return handler(ctx, req)
	}
}
