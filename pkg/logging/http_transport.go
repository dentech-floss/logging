package logging

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"strconv"
	"time"
)

const (
	logTypeLabel                = "log_type"
	logTypeValueExternalRequest = "external_request"
	loggingHTTPStatusCodeLabel  = "http_status_code"
)

type LoggingTransport struct {
	rt http.RoundTripper
	l  *Logger
}

func NewLoggingTransport(base http.RoundTripper, logger *Logger) *LoggingTransport {
	return &LoggingTransport{rt: base, l: logger}
}

func (lt *LoggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	var loggerFields []any
	dumpedReq := dumpRequest(req)
	loggerFields = append(
		loggerFields,
		String("url", req.URL.String()),
		String("request", dumpedReq),
		String(logTypeLabel, logTypeValueExternalRequest),
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
		String(loggingHTTPStatusCodeLabel, strconv.Itoa(resp.StatusCode)),
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
