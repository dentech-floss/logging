# logging

Provides a [slog](https://pkg.go.dev/log/slog) logger, configured for GCP Cloud Logging format and integrated with OpenTelemetry tracing. If the incoming context contains a trace, log messages will be recorded as events on the span, and log entries in GCP will include the "trace_id" field for improved observability.

The logger is also configured to support [Error Reporting](https://cloud.google.com/error-reporting) in GCP, automatically formatting error logs for reporting.

## Install

```
go get github.com/dentech-floss/logging@v0.3.7
```

## Usage

Create the logger once in `main` and inject it where it's needed:

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

    patientGatewayServiceV1 := service.NewPatientGatewayServiceV1(logger) // inject it
}
```

### Request fields in the context

Log with the injected logger and always pass `ctx` to the `*Context` methods (`InfoContext`, `ErrorContext`, ...).
Fields that should be on every log entry of a request, such as the caller, go into the context with
`logging.ContextWithFields`. Every entry logged with that context includes them, also in helpers further down the
call chain, and you don't have to pass a logger around:

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
    ctx = logging.ContextWithFields(ctx, logging.String("caller", caller.Name))

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
    // Includes "caller"
    s.logger.InfoContext(ctx, "Finding appointments", logging.Any("start_time_local", startTimeLocal))

    return &patient_gateway_service_v1.FindAppointmentsResponse{}, nil
}
```

`ContextWithFields` adds to the fields already in the context. Only fields added with `ContextWithFields` are
logged. The logger never logs anything else it finds in the context, apart from the OpenTelemetry trace and span
IDs.

For gRPC services, [dentech-floss/server](https://github.com/dentech-floss/server) can add the method and the
request to the context of every call, so handlers don't have to. See its `WithRequestLogFields` option.

### Logging protobuf messages

`logging.Proto` logs a protobuf message as JSON:

```go
s.logger.InfoContext(ctx, "Publishing patient event", logging.Proto("event", event))
```

The message is serialised when the first entry using the field is written, and only once. If nothing is logged
(for example a request field in the context of a call that logs nothing), it's never serialised.

### Redacting sensitive fields

Personal data, health information and secrets must not end up in logs. Instead of guessing from what a value looks
like (regexes for emails or personal numbers miss formats, and can't recognise names or free text at all), you
mark the sensitive fields where they are defined: in the `.proto` schema. `logging.Proto` always leaves those
fields out. There's nothing to turn on in the code.

#### 1. Mark the fields in the `.proto` file

Add the standard `debug_redact` option to every sensitive field. It's built into protobuf (buf, or protoc 22 or
newer), so no import is needed:

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

#### 2. Regenerate the Go code

Run `buf generate`, or push the schema to buf.build and update the package. The option is stored in the generated
code, so every `logging.Proto` call that logs these messages leaves the marked fields out from then on.

What to mark:

- Identifiers for a person: personal number (personnummer, samordningsnummer), name, email, phone, street address,
  date of birth.
- Health information: diagnoses, treatments tied to a person, symptoms.
- Free-text fields such as notes, comments and messages. They can contain any of the above.
- Secrets: passwords, tokens, API keys, card numbers.

You can mark a whole message field (e.g. `Patient patient = 1 [debug_redact = true];`) to leave all of it out.

#### What the output looks like

With the `Patient` message above, this message:

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
  `Any`.
- Redaction only applies to `logging.Proto`. A sensitive value logged with `logging.String`, `logging.Any` and so
  on is logged as is.

### Upgrading from v0.3.x

v0.4.0 has a few breaking changes. Most services only need to change a few lines in `main` and in code that logs
protobuf messages.

- **`logging.Proto` leaves out `debug_redact` fields and serialises lazily.** No code change is needed. If a
  message can't be serialised, the field now holds an error text instead of being replaced by an `error` field.
- **`ContextWithLoggerFields` and `LoggerFieldsFromContext` are removed.** Use `ContextWithFields`, which adds to
  the fields already in the context instead of replacing them.
- **`LoggerFromContext` no longer returns `nil`.** When the context holds no logger, it returns a fallback logger
  writing JSON to stdout at Info level. Code that checks the result for `nil` won't take that branch anymore.
- **`ContextWithLogger` and `LoggerFromContext` are deprecated**, and staticcheck (SA1019) and IDEs will flag them.
  They still work. Migrate when convenient:

  ```go
  // Before
  log := s.logger.With(logging.String("caller", caller.Name))
  ctx = logging.ContextWithLogger(ctx, log) // forgetting this made LoggerFromContext return nil
  ...
  log := logging.LoggerFromContext(ctx)
  log.ErrorContext(ctx, "Something failed", logging.Error(err))

  // After
  ctx = logging.ContextWithFields(ctx, logging.String("caller", caller.Name))
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
