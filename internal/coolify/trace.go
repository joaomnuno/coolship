package coolify

import (
	"net/http"
	"strings"
	"time"
)

// Exchange is one HTTP attempt as the client saw it, reported to the function
// WithTrace installs. A retried read is one Exchange per attempt. The bearer
// token never appears: RequestHeader carries Authorization masked to the
// token's last four characters.
type Exchange struct {
	Method string
	URL    string
	// Attempt is 0 for the first request and counts retries after it.
	Attempt       int
	RequestHeader http.Header
	RequestBody   []byte
	// Status is 0 when no response arrived; Err then says why.
	Status         int
	ResponseHeader http.Header
	// ResponseBody is what the client read of the response, which is
	// nothing when it stopped before the body.
	ResponseBody []byte
	Duration     time.Duration
	Err          error
}

// WithTrace reports every request attempt to trace after it ends. It is meant
// for --verbose and --debug; without it the client reports nothing and reads
// no more of a refusal than it needs.
func WithTrace(trace func(Exchange)) Option {
	return func(c *Client) { c.trace = trace }
}

// maskToken keeps the last four characters of the token so a reader can tell
// which token was sent without the token being printed.
func maskToken(token string) string {
	if len(token) <= 4 {
		return strings.Repeat("*", len(token))
	}
	return "****" + token[len(token)-4:]
}

// traceHeader copies the request headers with the token masked.
func (c *Client) traceHeader(header http.Header) http.Header {
	copied := header.Clone()
	if copied.Get("Authorization") != "" {
		copied.Set("Authorization", "Bearer "+maskToken(c.token))
	}
	return copied
}
