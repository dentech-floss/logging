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
	DumpRequestOut bool
	DumpResponse   bool
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
	var loggerFields []any
	dumpedReq := dumpRequest(req)
	loggerFields = append(
		loggerFields,
		Label("log_type", logTypeValueExternalRequest),
		String("url", req.URL.String()),
		String("request", dumpedReq),
		Any("request_headers", req.Header),
	)
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
		lt.l.ErrorContext(
			ctx,
			"call to external service FAILED",
			loggerFields...,
		)
		return nil, err
	}
	dumpedResp := dumpResponse(resp)
	loggerFields = append(
		loggerFields,
		String("response", dumpedResp),
		Int("status", resp.StatusCode),
		Any("response_headers", resp.Header),
	)

	lt.l.InfoContext(
		ctx,
		"called external service",
		loggerFields...,
	)

	return resp, nil
}

func dumpRequest(
	req *http.Request,
) string {
	reqDump, err := httputil.DumpRequestOut(req, true)
	if err == nil {
		return string(reqDump)
	}

	return fmt.Sprintf("Error dumping request: %v", err)
}

func dumpResponse(resp *http.Response) string {
	respDump, err := httputil.DumpResponse(resp, true)
	if err == nil {
		return string(respDump)
	}
	return fmt.Sprintf("Error dumping response: %v", err)
}
