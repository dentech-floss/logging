# logging

Provides a [slog](https://pkg.go.dev/log/slog) logger, configured for GCP Cloud Logging format and integrated with OpenTelemetry tracing. If the incoming context contains a trace, log messages will be recorded as events on the span, and log entries in GCP will include the "trace_id" field for improved observability.

The logger is also configured to support [Error Reporting](https://cloud.google.com/error-reporting) in GCP, automatically formatting error logs for reporting.

## Install

```
go get github.com/dentech-floss/logging@v0.1.0
```

## Usage

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
            ServiceName: revision.ServiceName,
            MinLevel:    logging.InfoLevel,
        },
    )
    defer logger.Sync() // flushes buffer, if any

    patientGatewayServiceV1 := service.NewPatientGatewayServiceV1(logger) // inject it
}
```

```go
package example

import (
    "github.com/dentech-floss/logging/pkg/logging"

    patient_gateway_service_v1 "go.buf.build/dentechse/go-grpc-gateway-openapiv2/dentechse/patient-api-gateway/api/patient/v1"
)

func (s *PatientGatewayServiceV1) FindAppointments(
    ctx context.Context,
    request *patient_gateway_service_v1.FindAppointmentsRequest,
) (*patient_gateway_service_v1.FindAppointmentsResponse, error) {

    // Ensure trace information + request is part of the log entries
    logWithContext := s.logger.WithContext(ctx, logging.Proto("request", request))

    logWithContext.Info(
        "Something something...",
        logging.String("something", something),
    )

    startTimeLocal, err := datetime.ISO8601StringToTime(request.StartTime)
    if err != nil {
        logWithContext.Warn("The start time shall be in ISO 8601 format", logging.Error(err))
        return &patient_gateway_service_v1.FindAppointmentsResponse{},
            status.Errorf(codes.InvalidArgument, "The start time shall be in ISO 8601 format")
    }
}

```
