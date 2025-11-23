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

// DumpRequest returns a string representation of an HTTP request,
// including headers and body. If dumping fails, it returns an error message.
func DumpRequest(
	req *http.Request,
) (string, error) {
	if req == nil {
		return "", fmt.Errorf("nil request")
	}
	reqDump, err := httputil.DumpRequestOut(req, true)
	if err == nil {
		return string(reqDump), nil
	}

	return "", fmt.Errorf("failed dumping request: %w", err)
}

// DumpResponse returns a string representation of an HTTP response,
// including headers and body. If dumping fails, it returns an error message.
func DumpResponse(resp *http.Response) (string, error) {
	if resp == nil {
		return "", fmt.Errorf("nil response")
	}
	respDump, err := httputil.DumpResponse(resp, true)
	if err == nil {
		return string(respDump), nil
	}
	return "", fmt.Errorf("failed dumping response: %w", err)
}
