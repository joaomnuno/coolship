// Package coolify adapts the Coolify HTTP API without reading local configuration.
package coolify

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"syscall"
)

// HTTPError describes an unsuccessful response without including its
// headers, instance URL, or credentials. Message is the server's own
// explanation, kept only when a mutation is refused with a 4xx status, which
// the caller must act on (a validation failure, a repository Coolify cannot
// reach); reads and server faults carry the status alone, so a body that is
// not an explanation is never repeated.
//
// A 403 is read on every method, because Coolify answers its access checks
// with fixed sentences that say what to change: Denial names which one, and
// Abilities lists the permissions a "Missing required permissions" refusal
// named. Message keeps that sentence; any other 403 body of a read is dropped
// as before. Instance is the instance's root URL (never credentials, no
// /api/v1), kept so a caller can point at a Coolify page; it is not part of
// the text.
type HTTPError struct {
	StatusCode int
	Method     string
	Endpoint   string
	Message    string
	Denial     Denial
	Abilities  []string
	Instance   string
}

// Denial is the access check a 403 from Coolify's API failed.
type Denial string

const (
	// DenialAPIDisabled is "API is disabled.": API access is turned off in
	// the instance settings.
	DenialAPIDisabled Denial = "api_disabled"
	// DenialIPNotAllowed is "You are not allowed to access the API.": the
	// client address is outside the instance's API allowlist.
	DenialIPNotAllowed Denial = "ip_not_allowed"
	// DenialMissingPermissions is "Missing required permissions: a, b": the
	// token lacks an ability the endpoint requires.
	DenialMissingPermissions Denial = "missing_permissions"
	// DenialTokenExceedsRole refuses a token whose abilities exceed its
	// owner's role as a team member, who may only hold read tokens.
	DenialTokenExceedsRole Denial = "token_exceeds_role"
)

// Refusal names the failed access check, or "" for any other response, so
// callers can classify a 403 without importing this package.
func (e *HTTPError) Refusal() string { return string(e.Denial) }

const missingPermissionsPrefix = "Missing required permissions:"

// parseDenial recognizes the sentences Coolify's ApiAllowed and ApiAbility
// middleware put in a 403 body. The abilities are read from the sentence,
// the only place the response carries them.
func parseDenial(message string) (Denial, []string) {
	switch {
	case message == "API is disabled.":
		return DenialAPIDisabled, nil
	case message == "You are not allowed to access the API.":
		return DenialIPNotAllowed, nil
	case strings.HasPrefix(message, missingPermissionsPrefix):
		var abilities []string
		for _, ability := range strings.Split(strings.TrimPrefix(message, missingPermissionsPrefix), ",") {
			if ability = strings.TrimSpace(ability); ability != "" {
				abilities = append(abilities, ability)
			}
		}
		return DenialMissingPermissions, abilities
	case strings.HasPrefix(message, "This API token has permissions (") && strings.Contains(message, "exceed your current role"):
		return DenialTokenExceedsRole, nil
	}
	return "", nil
}

// HTTPStatusCode lets callers classify a failure without importing this package.
func (e *HTTPError) HTTPStatusCode() int { return e.StatusCode }

func (e *HTTPError) Error() string {
	text := fmt.Sprintf("Coolify %s %s: HTTP %d %s", e.Method, e.Endpoint, e.StatusCode, http.StatusText(e.StatusCode))
	if e.Message != "" {
		text += ": " + e.Message
	}
	return text
}

// NotRunningError reports that the server refused runtime logs because the
// application has no running container (HTTP 400 with its "not running"
// explanation). Message is the server's sentence, sanitized and capped.
type NotRunningError struct {
	Message string
}

func (e *NotRunningError) Error() string { return "application is not running" }

// NotRunning lets callers classify the failure without importing this package.
func (e *NotRunningError) NotRunning() bool { return true }

// RequestError retains the transport cause for errors.Is/As while keeping its
// error text private: custom transports can include credentials in errors.
// Common causes are named through a fixed allow-list of phrases, never through
// the transport's own text.
type RequestError struct {
	Method   string
	Endpoint string
	Err      error
}

func (e *RequestError) Error() string {
	return fmt.Sprintf("Coolify %s %s: %s", e.Method, e.Endpoint, describeTransport(e.Err))
}

func (e *RequestError) Unwrap() error { return e.Err }

// describeTransport maps the transport failures a wrong URL or network
// produces to fixed phrases. Anything else is "request failed": the cause
// stays reachable through Unwrap, but its text may carry a URL with
// credentials or a proxy's own message, so it is never repeated.
func describeTransport(err error) string {
	switch transportKind(err) {
	case TransportInterrupted:
		return "request interrupted"
	case TransportTimeout:
		if errors.Is(err, context.DeadlineExceeded) {
			return "request timed out"
		}
		return "the request timed out; check the instance URL and the network"
	case TransportHostNotFound:
		return "the host name could not be resolved; check the instance URL"
	case TransportRefused:
		return "the connection was refused; check the instance URL and that Coolify is running"
	case TransportCertificate:
		return "the TLS certificate could not be verified; check the instance URL"
	case TransportNotTLS:
		return "the server did not answer with TLS; check the instance URL (http or https)"
	case TransportHandshake:
		return "the server refused the TLS handshake; check the instance URL (its certificate may not cover this host name)"
	}
	return "request failed"
}

// TransportKind names the cause of a request that got no response, from the
// same fixed allow-list the text uses.
type TransportKind string

const (
	TransportFailed       TransportKind = "failed"
	TransportInterrupted  TransportKind = "interrupted"
	TransportTimeout      TransportKind = "timeout"
	TransportHostNotFound TransportKind = "host_not_found"
	TransportRefused      TransportKind = "connection_refused"
	TransportCertificate  TransportKind = "tls_certificate"
	TransportNotTLS       TransportKind = "not_tls"
	TransportHandshake    TransportKind = "tls_handshake"
)

// Kind classifies the transport cause without exposing its text.
func (e *RequestError) Kind() TransportKind { return transportKind(e.Err) }

func transportKind(err error) TransportKind {
	var dnsError *net.DNSError
	var certificateError *tls.CertificateVerificationError
	var unknownAuthority x509.UnknownAuthorityError
	var hostnameError x509.HostnameError
	var certificateInvalid x509.CertificateInvalidError
	var recordHeader tls.RecordHeaderError
	var timeout interface{ Timeout() bool }
	switch {
	case err == nil:
		return TransportFailed
	case errors.Is(err, context.Canceled):
		return TransportInterrupted
	case errors.Is(err, context.DeadlineExceeded):
		return TransportTimeout
	case errors.As(err, &dnsError):
		return TransportHostNotFound
	case errors.Is(err, syscall.ECONNREFUSED):
		return TransportRefused
	case errors.As(err, &certificateError), errors.As(err, &unknownAuthority), errors.As(err, &hostnameError), errors.As(err, &certificateInvalid):
		return TransportCertificate
	case errors.As(err, &recordHeader), isPlainHTTPAnswer(err):
		return TransportNotTLS
	case isRemoteTLSAlert(err):
		return TransportHandshake
	case errors.As(err, &timeout) && timeout.Timeout():
		return TransportTimeout
	}
	return TransportFailed
}

// plainHTTPAnswer is the fixed text net/http uses when an https request
// reaches a server that speaks plain HTTP; it has no exported sentinel.
const plainHTTPAnswer = "http: server gave HTTP response to HTTPS client"

// isPlainHTTPAnswer matches that one known message exactly; it repeats no
// text of its own.
func isPlainHTTPAnswer(err error) bool {
	for e := err; e != nil; e = errors.Unwrap(e) {
		if e.Error() == plainHTTPAnswer {
			return true
		}
	}
	return false
}

// isRemoteTLSAlert recognizes an alert the server sent during the handshake,
// which crypto/tls reports as a net.OpError whose operation is "remote error".
// The alert itself is not exported, so the operation is the stable signal.
func isRemoteTLSAlert(err error) bool {
	var op *net.OpError
	return errors.As(err, &op) && op.Op == "remote error"
}

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
