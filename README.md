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
    // Includes "caller" (and "grpc.method" with the interceptor below)
    s.logger.InfoContext(ctx, "Finding appointments", logging.Any("start_time_local", startTimeLocal))

    return &patient_gateway_service_v1.FindAppointmentsResponse{}, nil
}
```

`WithFields` adds to the fields already in the context. Only fields added with `WithFields` (or
`ContextWithLoggerFields`) are logged. The logger never logs anything else it finds in the context, apart from the
OpenTelemetry trace and span IDs.

### gRPC interceptor

`grpclogging.UnaryServerInterceptor` adds `grpc.method` to the context of every unary call, so it's on every entry
logged during the call:

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

The request is **not** logged by default. To add it as the `request` field, pass one of these options:

| Option | Logs |
|---|---|
| `WithRedactedRequest()` | The request, without the fields marked as sensitive in the `.proto` schema. **Use this one.** See [Redacting sensitive fields](#redacting-sensitive-fields). |
| `WithRequestFilter(filter)` | Whatever your filter returns, or nothing. Use it to skip some methods entirely. |
| `WithRequest()` | The full request, unredacted. Only for services whose requests never contain sensitive data. |

```go
grpclogging.UnaryServerInterceptor(grpclogging.WithRedactedRequest())
```

> **Warning:** `WithRequest` writes the full request, in plain text, to every entry logged during the call. Don't
> use it in services whose requests contain personal data (e.g. patient information), payment details or secrets.
> Use `WithRedactedRequest` instead.

### Redacting sensitive fields

Personal data, health information and secrets must not end up in logs. Instead of guessing from what a value looks
like (regexes for emails or personal numbers miss formats, and can't recognise names or free text at all), you
mark the sensitive fields where they are defined: in the `.proto` schema. The logger leaves those fields out.

#### 1. Mark the fields in the `.proto` file

Add the standard `debug_redact` option to every sensitive field. It's built into protobuf (buf, or protoc 22 or newer), so no import is needed:

```proto
message Patient {
  string id = 1;
  string personal_number = 2 [debug_redact = true];
  string name = 3 [debug_redact = true];
  string email = 4 [debug_redact = true];
  string phone = 5 [debug_redact = true];
  Address address = 6;
  string notes = 7 [debug_redact = true]; // free text can contain anything
}

message Address {
  string street = 1 [debug_redact = true];
  string postal_code = 2;
  string city = 3;
}
```

Then regenerate the Go code (e.g. `buf generate`, or push the schema to buf.build and update the package). The
option is stored in the generated code, so nothing else is needed.

What to mark:

- Identifiers for a person: personal number (personnummer, samordningsnummer), name, email, phone, street address,
  date of birth.
- Health information: diagnoses, treatments tied to a person, symptoms.
- Free-text fields such as notes, comments and messages. They can contain any of the above.
- Secrets: passwords, tokens, API keys, card numbers.

You can mark a whole message field (e.g. `Patient patient = 1 [debug_redact = true];`) to leave all of it out.

#### 2. Log with redaction

For gRPC requests, register the interceptor with `WithRedactedRequest()`:

```go
_server := server.NewServer(&server.ServerConfig{
    Port: port,
    GrpcServerOptions: []grpc.ServerOption{
        grpc.ChainUnaryInterceptor(
            grpclogging.UnaryServerInterceptor(grpclogging.WithRedactedRequest()),
        ),
    },
})
```

Anywhere else you log a protobuf message (Pub/Sub events, responses, calls to other services), use
`logging.RedactedProto` instead of `logging.Proto`:

```go
s.logger.InfoContext(ctx, "Publishing patient event", logging.RedactedProto("event", event))
```

To skip some methods entirely and redact the rest, combine it with a filter:

```go
grpclogging.UnaryServerInterceptor(grpclogging.WithRequestFilter(
    func(info *grpc.UnaryServerInfo, req proto.Message) (slog.Attr, bool) {
        if strings.HasPrefix(info.FullMethod, "/api.payment.v1.") {
            return slog.Attr{}, false // never log payment requests
        }
        return logging.RedactedProto("request", req), true
    },
))
```

#### What the output looks like

With the `Patient` message above, this request:

```json
{"id": "42", "personalNumber": "199001011234", "name": "Anna Andersson", "address": {"street": "Storgatan 1", "postalCode": "11122", "city": "Stockholm"}}
```

is logged as:

```json
{"id": "42", "address": {"postalCode": "11122", "city": "Stockholm"}}
```

Redacted fields are left out, not replaced with a placeholder. A missing field in the log means it was either
empty or redacted.

#### Good to know

- Redaction works on nested messages, lists and maps at any depth. The message you pass in is not modified.
- Only marked fields are removed. When you add a new field to a message, decide whether it needs
  `debug_redact = true`. Make it part of reviewing `.proto` changes.
- Fields inside `google.protobuf.Any` values are not inspected. Don't log messages that carry sensitive data in an
  `Any`, or skip them with a filter.
- `logging.Proto` and `WithRequest` ignore `debug_redact`. Use `logging.RedactedProto` and `WithRedactedRequest`.

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
