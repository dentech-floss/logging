# logging

Provides a [zap](https://github.com/uber-go/zap) logger, adjusted for the GCP Cloud Logging format by [zapdriver](https://github.com/blendle/zapdriver) and wrapped by [otelzap](https://github.com/uptrace/opentelemetry-go-extra/tree/main/otelzap) to provide Opentelemetry tracing/logging support. Meaning that if the incoming context contains a trace then Zap log messages will be recorded as events on the span. The log entries in GCP will also include the "trace_id" as a field to further increase the observability.

About GCP Cloud Logging, the logger is configured to use [Error Reporting](https://cloud.google.com/error-reporting) as described [here](https://github.com/blendle/zapdriver#using-error-reporting).

## Install

```
go get github.com/dentech-floss/logging@v0.1.0
```

## Usage

```go
package example

import (
    "github.com/dentech-floss/logging/pkg/logging"
)

func main() {
    logger := logging.NewLogger(
        &logging.LoggerConfig{
            OnGCP: true,
            ServiceName: "mysuperduper-service",
        },
    )
    defer logger.Sync() // flushes buffer, if any
}
```

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

    // Ensure trace information + request is part of the log entries
    logWithContext := s.logger.WithContext(
		ctx,
		logging.ProtoField("request", request),
	)

    logWithContext.Info(
        "Something something...",
        logging.StringField("something", something),
    )

    startTimeLocal, err := datetime.ISO8601StringToTime(request.StartTime)
    if err != nil {
        logWithContext.Warn("The start time shall be in ISO 8601 format", logging.ErrorField(err))
        return &patient_gateway_service_v1.FindAppointmentsResponse{},
            status.Errorf(codes.InvalidArgument, "The start time shall be in ISO 8601 format")
    }
}

```