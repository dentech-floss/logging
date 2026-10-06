# logging

Provides a [slog](https://pkg.go.dev/log/slog) logger, configured for GCP Cloud Logging format and integrated with OpenTelemetry tracing. If the incoming context contains a trace, log messages will be recorded as events on the span, and log entries in GCP will include the "trace_id" field for improved observability.

The logger is also configured to support [Error Reporting](https://cloud.google.com/error-reporting) in GCP, automatically formatting error logs for reporting.

## Install

```
go get github.com/dentech-floss/logging@v0.3.7
```

## Usage

Create the logger once in `main`, make it the default and inject it where it's needed:

```go
package example

import (
    "github.com/dentech-floss/metadata/pkg/metadata"
    "github.com/dentech-floss/logging/pkg/logging"
    "github.com/dentech-floss/revision/pkg/revision"
)

func main() {

    metadata := metadata.NewMetadata()

    logger := logging.NewLogger(
        &logging.LoggerConfig{
            ProjectID:    metadata.ProjectID,
            ServiceName: revision.ServiceName,
            MinLevel:    logging.InfoLevel,
        },
    )
    logging.SetDefault(logger) // used by LoggerFromContext when the context holds no logger

    patientGatewayServiceV1 := service.NewPatientGatewayServiceV1(logger) // inject it
}
```

### Request fields in the context

Log with the injected logger and always pass `ctx` to the `*Context` methods (`InfoContext`, `ErrorContext`, ...).
Fields that should be on every log entry of a request, such as the caller, go into the context with
`logging.WithFields`. Every entry logged with that context includes them, also in helpers further down the call
chain, and you don't have to pass a logger around:

```go
package example

import (
    "context"

    "github.com/dentech-floss/logging/pkg/logging"

    patient_gateway_service_v1 "go.buf.build/dentechse/go-grpc-gateway-openapiv2/dentechse/patient-api-gateway/api/patient/v1"
)

func (s *PatientGatewayServiceV1) FindAppointments(
    ctx context.Context,
    request *patient_gateway_service_v1.FindAppointmentsRequest,
) (*patient_gateway_service_v1.FindAppointmentsResponse, error) {

    caller, err := s.callerService.GetCurrent(ctx, request)
    if err != nil {
        return nil, err
    }

    // Added to every log entry below, and in s.findAppointments, that uses this ctx
    ctx = logging.WithFields(ctx, logging.String("caller", caller.Name))

    startTimeLocal, err := datetime.ISO8601StringToTime(request.StartTime)
    if err != nil {
        s.logger.WarnContext(ctx, "The start time shall be in ISO 8601 format", logging.Error(err))
        return &patient_gateway_service_v1.FindAppointmentsResponse{},
            status.Errorf(codes.InvalidArgument, "The start time shall be in ISO 8601 format")
    }

    return s.findAppointments(ctx, startTimeLocal)
}

func (s *PatientGatewayServiceV1) findAppointments(
    ctx context.Context,
    startTimeLocal time.Time,
) (*patient_gateway_service_v1.FindAppointmentsResponse, error) {
    // Includes "caller" (and "grpc.method" + "request" with the interceptor below)
    s.logger.InfoContext(ctx, "Finding appointments", logging.Any("start_time_local", startTimeLocal))

    return &patient_gateway_service_v1.FindAppointmentsResponse{}, nil
}
```

`WithFields` adds to the fields already in the context. Only fields added with `WithFields` (or
`ContextWithLoggerFields`) are logged. The logger never logs anything else it finds in the context, apart from the
OpenTelemetry trace and span IDs.

### gRPC interceptor

`grpclogging.UnaryServerInterceptor` adds `grpc.method` and `request` (when the request is a protobuf message) to
the context of every unary call, so handlers don't have to:

```go
import (
    "github.com/dentech-floss/logging/pkg/logging/grpclogging"
    "github.com/dentech-floss/server/pkg/server"
    "google.golang.org/grpc"
)

_server := server.NewServer(&server.ServerConfig{
    Port: port,
    GrpcServerOptions: []grpc.ServerOption{
        grpc.ChainUnaryInterceptor(grpclogging.UnaryServerInterceptor()),
    },
})
```

The request is attached to every entry logged during the call. Keep that in mind for methods whose requests
carry sensitive data.

### Upgrading from v0.3.x

- `LoggerFromContext` no longer returns `nil`. When the context holds no logger it returns `logging.Default()`,
  the logger passed to `SetDefault` (or, if `SetDefault` was never called, a JSON logger on stdout at Info level).
  Code that checks the result for `nil` will no longer take that branch. Call `SetDefault` in `main` so the
  fallback has your service's config.
- `ContextWithLogger` and `LoggerFromContext` are deprecated, and staticcheck (SA1019) and IDEs will flag them.
  They still work. Migrate when convenient:

  ```go
  // Before
  log := s.logger.With(logging.String("caller", caller.Name))
  ctx = logging.ContextWithLogger(ctx, log) // forgetting this made LoggerFromContext return nil
  ...
  log := logging.LoggerFromContext(ctx)
  log.ErrorContext(ctx, "Something failed", logging.Error(err))

  // After
  ctx = logging.WithFields(ctx, logging.String("caller", caller.Name))
  ...
  s.logger.ErrorContext(ctx, "Something failed", logging.Error(err))
  ```

### HTTP client transport

```go
import (
    "net/http"

    "github.com/dentech-floss/logging/pkg/logging"
    "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Example of wrapping an HTTP client's Transport with NewLoggingTransport
httpClient := &http.Client{}
httpClient.Transport = otelhttp.NewTransport(
    logging.NewLoggingTransport(
        httpClient.Transport,
        logger,
        &logging.LoggingOptions{
            DumpRequestFunc:  logging.DumpRequest,
            DumpResponseFunc: logging.DumpResponse,
        },
    ),
)
```
