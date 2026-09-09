// Package coolify adapts the Coolify HTTP API without reading local configuration.
package coolify

import (
	"fmt"
	"net/http"
)

// HTTPError describes an unsuccessful response without including its potentially
// sensitive body, headers, instance URL, or credentials.
type HTTPError struct {
	StatusCode int
	Method     string
	Endpoint   string
}

// HTTPStatusCode lets callers classify a failure without importing this package.
func (e *HTTPError) HTTPStatusCode() int { return e.StatusCode }

func (e *HTTPError) Error() string {
	return fmt.Sprintf("Coolify %s %s: HTTP %d %s", e.Method, e.Endpoint, e.StatusCode, http.StatusText(e.StatusCode))
}

// RequestError retains the transport cause for errors.Is/As while keeping its
// error text private. Custom transports can include credentials in errors.
type RequestError struct {
	Method   string
	Endpoint string
	Err      error
}

func (e *RequestError) Error() string {
	return fmt.Sprintf("Coolify %s %s: request failed", e.Method, e.Endpoint)
}

func (e *RequestError) Unwrap() error { return e.Err }

// ProtocolError reports a response that cannot safely satisfy the API contract.
type ProtocolError struct {
	Endpoint string
	Reason   string
}

func (e *ProtocolError) Error() string {
	return fmt.Sprintf("Coolify %s: %s", e.Endpoint, e.Reason)
}

// UncertainSubmissionError means that Coolify may have accepted a deployment.
// Callers must not retry automatically, including when the cause is cancellation.
type UncertainSubmissionError struct {
	ResourceUUID string
	Err          error
}

func (e *UncertainSubmissionError) Error() string {
	return fmt.Sprintf("deployment submission for application %q is uncertain; inspect Coolify before retrying: %v", e.ResourceUUID, e.Err)
}

func (e *UncertainSubmissionError) Unwrap() error { return e.Err }
