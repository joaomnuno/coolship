package coolify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	maxRetries       = 5
	maxRetryDelay    = 30 * time.Second
	maxResponseBytes = 16 << 20
)

type Client struct {
	baseURL    *url.URL
	token      string
	http       *http.Client
	retries    int
	retryDelay time.Duration
	userAgent  string
}

// DefaultUserAgent identifies Coolship when no version is supplied.
const DefaultUserAgent = "coolship/dev"

type Option func(*Client)

// WithHTTPClient copies the supplied client. Redirects remain disabled so bearer
// credentials cannot escape the selected endpoint, even to a sibling host.
func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) {
		if client != nil {
			copy := *client
			c.http = &copy
		}
	}
}

// WithRetries sets the maximum number of extra GET attempts, bounded to five.
// Deployment requests never use this setting.
func WithRetries(retries int) Option {
	return func(c *Client) { c.retries = min(max(retries, 0), maxRetries) }
}

func WithRetryDelay(delay time.Duration) Option {
	return func(c *Client) { c.retryDelay = min(max(delay, 0), maxRetryDelay) }
}

// WithUserAgent identifies the client to the server and to any proxy in front
// of it. Control characters are rejected so the header cannot be split.
func WithUserAgent(agent string) Option {
	return func(c *Client) {
		if agent != "" && strings.IndexFunc(agent, unicode.IsControl) < 0 {
			c.userAgent = agent
		}
	}
}

func NewClient(baseURL, token string, opts ...Option) (*Client, error) {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.Opaque != "" {
		return nil, errors.New("Coolify URL must be an absolute HTTP or HTTPS URL")
	}
	if u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, errors.New("Coolify URL must not contain credentials, a query, or a fragment")
	}
	if token == "" || strings.IndexFunc(token, unicode.IsSpace) >= 0 || strings.IndexFunc(token, unicode.IsControl) >= 0 {
		return nil, errors.New("Coolify token must be nonempty and contain no whitespace or control characters")
	}
	path := strings.TrimRight(u.EscapedPath(), "/")
	if !strings.HasSuffix(path, "/api/v1") {
		path += "/api/v1"
	}
	u.Path, err = url.PathUnescape(path)
	if err != nil {
		return nil, errors.New("Coolify URL has an invalid path")
	}
	u.RawPath = path
	c := &Client{baseURL: u, token: token, http: &http.Client{Timeout: 30 * time.Second}, retries: 2, retryDelay: 200 * time.Millisecond, userAgent: DefaultUserAgent}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	c.http.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return c, nil
}

func (c *Client) endpoint(parts ...string) (*url.URL, string, error) {
	escaped := make([]string, len(parts))
	for i, part := range parts {
		if part == "" || part == "." || part == ".." {
			return nil, "", errors.New("Coolify resource identifiers must be nonempty path segments")
		}
		escaped[i] = url.PathEscape(part)
	}
	relative := "/" + strings.Join(escaped, "/")
	u := *c.baseURL
	u.RawPath = strings.TrimRight(c.baseURL.EscapedPath(), "/") + relative
	u.Path, _ = url.PathUnescape(u.RawPath)
	return &u, relative, nil
}

func (c *Client) request(ctx context.Context, method string, parts []string, query url.Values, body any, out any) error {
	data, endpoint, err := c.fetch(ctx, method, parts, query, body)
	if err != nil {
		return err
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) || json.Unmarshal(data, out) != nil {
		return &ProtocolError{Endpoint: endpoint, Reason: "response does not match the expected JSON contract"}
	}
	return nil
}

// fetch performs one bounded request and returns the raw successful body.
func (c *Client) fetch(ctx context.Context, method string, parts []string, query url.Values, body any) ([]byte, string, error) {
	u, endpoint, err := c.endpoint(parts...)
	if err != nil {
		return nil, "", err
	}
	u.RawQuery = query.Encode()
	var encoded []byte
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			return nil, "", fmt.Errorf("encode Coolify request: %w", err)
		}
	}
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(encoded))
		if err != nil {
			return nil, "", &RequestError{Method: method, Endpoint: endpoint, Err: err}
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", c.userAgent)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if method != http.MethodGet {
			req.GetBody = nil
		}
		resp, err := c.http.Do(req)
		if err != nil {
			if resp != nil && resp.Body != nil {
				resp.Body.Close()
			}
			if method == http.MethodGet && attempt < c.retries && ctx.Err() == nil {
				if err := wait(ctx, c.backoff(attempt)); err != nil {
					return nil, "", err
				}
				continue
			}
			return nil, "", &RequestError{Method: method, Endpoint: endpoint, Err: err}
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			message := ""
			if method != http.MethodGet && resp.StatusCode >= 400 && resp.StatusCode < 500 {
				message = serverMessage(resp.Body)
			}
			resp.Body.Close()
			delay, canRetry := c.retryAfter(resp.Header.Get("Retry-After"), attempt)
			if method == http.MethodGet && attempt < c.retries && retryable(resp.StatusCode) && canRetry {
				if err := wait(ctx, delay); err != nil {
					return nil, "", err
				}
				continue
			}
			return nil, "", &HTTPError{StatusCode: resp.StatusCode, Method: method, Endpoint: endpoint, Message: message}
		}
		if strings.Contains(resp.Header.Get("Link"), `rel="next"`) || strings.Contains(resp.Header.Get("Link"), "rel=next") {
			resp.Body.Close()
			return nil, "", &ProtocolError{Endpoint: endpoint, Reason: "unexpected pagination; refusing to use an incomplete resource list"}
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
		resp.Body.Close()
		if readErr != nil {
			return nil, "", &RequestError{Method: method, Endpoint: endpoint, Err: readErr}
		}
		if len(data) > maxResponseBytes {
			return nil, "", &ProtocolError{Endpoint: endpoint, Reason: "response exceeds the 16 MiB limit"}
		}
		return data, endpoint, nil
	}
}

// maxMessageBytes bounds what is read from a refusal; an explanation is short.
const maxMessageBytes = 64 << 10

// serverMessage extracts the explanation Coolify's API puts in a refusal:
// {"message": ..., "errors": {field: [text, ...]}}. Field errors are listed by
// name, since they say which request value to change. Anything that is not
// that shape yields nothing.
func serverMessage(body io.Reader) string {
	data, err := io.ReadAll(io.LimitReader(body, maxMessageBytes))
	if err != nil {
		return ""
	}
	var payload struct {
		Message string                     `json:"message"`
		Errors  map[string]json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(data, &payload) != nil {
		return ""
	}
	parts := []string{printable(payload.Message)}
	fields := make([]string, 0, len(payload.Errors))
	for field := range payload.Errors {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	for _, field := range fields {
		var texts []string
		var text string
		switch raw := payload.Errors[field]; {
		case json.Unmarshal(raw, &texts) == nil:
		case json.Unmarshal(raw, &text) == nil:
			texts = []string{text}
		}
		if joined := printable(strings.Join(texts, " ")); joined != "" {
			parts = append(parts, printable(field)+": "+joined)
		}
	}
	var kept []string
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	result := strings.Join(kept, "; ")
	if len(result) > 500 {
		result = result[:500] + "…"
	}
	return result
}

// printable keeps text that can be shown on one line of a terminal.
func printable(value string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value))
}

func retryable(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func (c *Client) backoff(attempt int) time.Duration {
	return min(c.retryDelay*time.Duration(1<<attempt), maxRetryDelay)
}

// Retry-After values beyond the bounded wait budget suppress the retry. Retrying
// earlier than the server's requested time would violate its backoff policy.
func (c *Client) retryAfter(value string, attempt int) (time.Duration, bool) {
	delay := c.backoff(attempt)
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds >= 0 {
		if seconds > int64(maxRetryDelay/time.Second) {
			return 0, false
		}
		delay = max(delay, time.Duration(seconds)*time.Second)
	} else if deadline, err := http.ParseTime(value); err == nil {
		delay = max(delay, time.Until(deadline))
	}
	return delay, delay <= maxRetryDelay
}

func wait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
