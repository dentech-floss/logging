package logging

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"time"
)

const (
	logTypeValueExternalRequest = "external_request"
)

type LoggingOptions struct {
	DumpRequestFunc  func(args []any, req *http.Request) []any
	DumpResponseFunc func(args []any, resp *http.Response) []any
}

type LoggingTransport struct {
	rt http.RoundTripper
	l  *Logger
	o  *LoggingOptions
}

func NewLoggingTransport(
	base http.RoundTripper,
	logger *Logger,
	options *LoggingOptions,
) *LoggingTransport {
	return &LoggingTransport{
		rt: base,
		l:  logger,
		o:  options,
	}
}

func (lt *LoggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	log := LoggerFromContext(ctx)
	if log == nil {
		log = lt.l
	}
	var loggerFields []any
	loggerFields = append(
		loggerFields,
		Label("log_type", logTypeValueExternalRequest),
		String("url", req.URL.String()),
	)

	if lt.o != nil && lt.o.DumpRequestFunc != nil {
		loggerFields = lt.o.DumpRequestFunc(loggerFields, req)
	}

	startTime := time.Now()
	resp, err := lt.rt.RoundTrip(req)
	duration := time.Since(startTime)
	loggerFields = append(
		loggerFields,
		Duration("duration", duration),
		Int64("duration_ms", duration.Milliseconds()),
	)
	if err != nil {
		loggerFields = append(loggerFields, Error(err))
		log.ErrorContext(
			ctx,
			"call to external service FAILED",
			loggerFields...,
		)
		return nil, err
	}
	if lt.o != nil && lt.o.DumpResponseFunc != nil {
		loggerFields = lt.o.DumpResponseFunc(loggerFields, resp)
	}
	loggerFields = append(
		loggerFields,
		Int("status", resp.StatusCode),
	)

	log.InfoContext(
		ctx,
		"called external service",
		loggerFields...,
	)

	return resp, nil
}

// DumpRequest appends a string representation of an HTTP request to the provided loggerFields slice.
// It includes both headers and body in the dump. If the request is nil or dumping fails, an error message
// is appended instead. The function returns the updated loggerFields slice.
//
// Parameters:
//   - loggerFields: a slice of fields to which the request dump or error will be appended.
//   - req: the HTTP request to be dumped.
//
// Returns:
//   - The updated loggerFields slice with the request dump or an error message.
func DumpRequest(
	loggerFields []any,
	req *http.Request,
) []any {
	if req == nil {
		loggerFields = append(
			loggerFields,
			String("request_dump_error", "Error dumping request: nil request"),
		)
	}

	reqDump, err := httputil.DumpRequestOut(req, true)
	if err == nil {
		loggerFields = append(
			loggerFields,
			String("request", string(reqDump)),
		)
		return loggerFields
	}

	loggerFields = append(
		loggerFields,
		String("request_dump_error", fmt.Sprintf("Error dumping request: %v", err)),
	)

	return loggerFields
}

// DumpResponse appends a string representation of an HTTP response to the provided loggerFields slice.
// It includes both headers and body in the dump. If the response is nil or dumping fails, an error message
// is appended instead. The function returns the updated loggerFields slice.
//
// Parameters:
//   - loggerFields: a slice of fields to which the response dump or error will be appended.
//   - resp: the HTTP response to be dumped.
//
// Returns:
//   - The updated loggerFields slice with the response dump or an error message.
func DumpResponse(loggerFields []any, resp *http.Response) []any {
	if resp == nil {
		loggerFields = append(
			loggerFields,
			String("response_dump_error", "Error dumping response: nil request"),
		)
		return loggerFields
	}

	respDump, err := httputil.DumpResponse(resp, true)
	if err == nil {
		loggerFields = append(
			loggerFields,
			String("response", string(respDump)),
		)

		return loggerFields
	}
	loggerFields = append(
		loggerFields,
		String("response_dump_error", fmt.Sprintf("Error dumping response: %v", err)),
	)
	return loggerFields
}
