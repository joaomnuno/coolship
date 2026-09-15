package coolify

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// healthEndpoint is Coolify's public liveness route. It sits beside the
// versioned API, not under /api/v1, and answers "OK" without a token.
const healthEndpoint = "/api/health"

// NotCoolifyError reports that the instance URL answered, but not the way a
// Coolify instance answers its health check: a status other than 200, or a
// body other than "OK". It usually means the URL points at another site.
type NotCoolifyError struct {
	Endpoint   string
	StatusCode int
}

func (e *NotCoolifyError) Error() string {
	if e.StatusCode != http.StatusOK {
		return fmt.Sprintf("Coolify GET %s: HTTP %d %s", e.Endpoint, e.StatusCode, http.StatusText(e.StatusCode))
	}
	return fmt.Sprintf("Coolify GET %s: the answer is not Coolify's", e.Endpoint)
}

// CheckHealth asks the instance at baseURL whether Coolify answers there,
// before any token is involved: GET /api/health, which Coolify serves without
// authentication. It sends no Authorization header, follows no redirect, and
// makes one attempt, so a wrong URL is reported at once. Failures are the
// same typed errors every other request returns: RequestError when nothing
// answered, HTTPError for a redirect or a server fault, and NotCoolifyError
// for any other answer.
func CheckHealth(ctx context.Context, baseURL string, opts ...Option) error {
	// The client wants a token; this request never sends it.
	client, err := NewClient(baseURL, "unused", opts...)
	if err != nil {
		return err
	}
	return client.Health(ctx)
}

// Health is CheckHealth on a configured client; the token is not sent.
func (c *Client) Health(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	address := strings.TrimRight(c.instance, "/") + healthEndpoint
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return &RequestError{Method: http.MethodGet, Endpoint: healthEndpoint, Err: err}
	}
	req.Header.Set("Accept", "text/plain, application/json")
	req.Header.Set("User-Agent", c.userAgent)
	started := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		c.reportHealth(req, nil, nil, started, err)
		return &RequestError{Method: http.MethodGet, Endpoint: healthEndpoint, Err: err}
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxMessageBytes))
	c.reportHealth(req, resp, body, started, readErr)
	switch code := resp.StatusCode; {
	case code >= 300 && code < 400, code >= 500:
		return &HTTPError{StatusCode: code, Method: http.MethodGet, Endpoint: healthEndpoint, Instance: c.instance}
	case code != http.StatusOK:
		return &NotCoolifyError{Endpoint: healthEndpoint, StatusCode: code}
	case readErr != nil:
		return &RequestError{Method: http.MethodGet, Endpoint: healthEndpoint, Err: readErr}
	case !strings.EqualFold(strings.TrimSpace(string(body)), "OK"):
		return &NotCoolifyError{Endpoint: healthEndpoint, StatusCode: code}
	}
	return nil
}

func (c *Client) reportHealth(req *http.Request, resp *http.Response, body []byte, started time.Time, err error) {
	if c.trace == nil {
		return
	}
	exchange := Exchange{Method: req.Method, URL: req.URL.String(), RequestHeader: c.traceHeader(req.Header),
		ResponseBody: c.traceBody(body), Duration: time.Since(started), Err: err}
	if resp != nil {
		exchange.Status, exchange.ResponseHeader = resp.StatusCode, resp.Header.Clone()
	}
	c.trace(exchange)
}
