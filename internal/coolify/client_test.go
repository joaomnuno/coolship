package coolify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/joaomnuno/coolship/internal/models"
	"time"
)

func newTestClient(t *testing.T, handler http.HandlerFunc, opts ...Option) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	options := []Option{WithRetries(0)}
	options = append(options, opts...)
	client, err := NewClient(server.URL, "fixture-token", options...)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestEndpointContracts(t *testing.T) {
	requests := make(map[string]int)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer fixture-token" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q", got)
		}
		key := r.Method + " " + r.URL.Path
		requests[key]++
		w.Header().Set("Content-Type", "application/json")
		switch key {
		case "GET /prefix/api/v1/projects":
			fmt.Fprint(w, `[{"uuid":"p1","name":"Personal","description":"unused"}]`)
		case "GET /prefix/api/v1/projects/p1/environments":
			fmt.Fprint(w, `[{"uuid":"e1","name":"production"}]`)
		case "GET /prefix/api/v1/projects/p1/e1":
			fmt.Fprint(w, `{"uuid":"e1","name":"production","applications":[{"uuid":"a1","name":"web"}]}`)
		case "GET /prefix/api/v1/applications/a1":
			fmt.Fprint(w, `{"uuid":"a1","name":"web","status":"running:healthy","fqdn":"https://example.test","unused_secret":"ignored"}`)
		case "POST /prefix/api/v1/deploy":
			if r.Header.Get("Content-Type") != "application/json" || r.URL.RawQuery != "" {
				t.Errorf("deploy headers/query: %v %q", r.Header.Get("Content-Type"), r.URL.RawQuery)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["uuid"] != "a1" || body["force"] != true || len(body) != 2 {
				t.Errorf("deploy body = %#v", body)
			}
			fmt.Fprint(w, `{"deployments":[{"resource_uuid":"a1","deployment_uuid":"d1","message":"queued"}]}`)
		case "GET /prefix/api/v1/deployments/d1":
			fmt.Fprint(w, `{"deployment_uuid":"d1","status":"finished"}`)
		case "GET /prefix/api/v1/applications/a1/logs":
			if r.URL.Query().Get("lines") != "25" || r.URL.Query().Get("show_timestamps") != "true" || len(r.URL.Query()) != 2 {
				t.Errorf("logs query = %v", r.URL.Query())
			}
			fmt.Fprint(w, `{"logs":"2026-09-09T12:00:00Z ready\n"}`)
		default:
			t.Errorf("unexpected endpoint %s", key)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client, err := NewClient(server.URL+"/prefix/api/v1/", "fixture-token")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if values, err := client.ListProjects(ctx); err != nil || len(values) != 1 || values[0].UUID != "p1" {
		t.Fatalf("projects = %#v, %v", values, err)
	}
	if values, err := client.ListEnvironments(ctx, "p1"); err != nil || len(values) != 1 || values[0].UUID != "e1" {
		t.Fatalf("environments = %#v, %v", values, err)
	}
	if value, err := client.GetEnvironment(ctx, "p1", "e1"); err != nil || value.UUID != "e1" || len(value.Applications) != 1 {
		t.Fatalf("environment = %#v, %v", value, err)
	}
	if value, err := client.GetApplication(ctx, "a1"); err != nil || value.UUID != "a1" || value.Status != "running:healthy" {
		t.Fatalf("application = %#v, %v", value, err)
	}
	if values, err := client.Deploy(ctx, models.DeployRequest{ApplicationUUID: "a1", Force: true}); err != nil || len(values) != 1 || values[0].DeploymentUUID != "d1" || values[0].ResourceUUID != "a1" {
		t.Fatalf("deploy = %#v, %v", values, err)
	}
	if value, err := client.GetDeployment(ctx, "d1"); err != nil || value.UUID != "d1" || value.Logs != nil {
		t.Fatalf("deployment = %#v, %v", value, err)
	}
	if value, err := client.Logs(ctx, "a1", 25); err != nil || value.Logs != "2026-09-09T12:00:00Z ready\n" {
		t.Fatalf("logs = %#v, %v", value, err)
	}
	if len(requests) != 7 {
		t.Errorf("endpoint count = %d, want 7", len(requests))
	}
}

func TestURLNormalizationAndEscapedIdentifiers(t *testing.T) {
	for _, suffix := range []string{"", "/", "/api/v1", "/api/v1/", "/proxy", "/proxy/api/v1/"} {
		t.Run(suffix, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				prefix := ""
				if strings.HasPrefix(suffix, "/proxy") {
					prefix = "/proxy"
				}
				if got, want := r.URL.EscapedPath(), prefix+"/api/v1/applications/a%2Fb%3F%23%25"; got != want {
					t.Errorf("path = %q, want %q", got, want)
				}
				if r.URL.RawQuery != "" {
					t.Errorf("unexpected query %q", r.URL.RawQuery)
				}
				fmt.Fprint(w, `{"uuid":"a/b?#%","name":"web"}`)
			}))
			defer server.Close()
			client, err := NewClient(server.URL+suffix, "fixture-token")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.GetApplication(context.Background(), "a/b?#%"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRejectInvalidClientInputsWithoutSecrets(t *testing.T) {
	for _, value := range []struct{ url, token string }{
		{"", "fixture-token"}, {"example.test", "fixture-token"}, {"ftp://example.test", "fixture-token"},
		{"https://user:secret@example.test", "fixture-token"}, {"https://example.test?token=secret", "fixture-token"},
		{"https://example.test#secret", "fixture-token"}, {"https://example.test", ""},
		{"https://example.test", "secret\r\nheader"}, {"https://example.test", "secret token"},
	} {
		_, err := NewClient(value.url, value.token)
		if err == nil {
			t.Errorf("accepted invalid client input")
		} else if strings.Contains(err.Error(), "secret") {
			t.Errorf("error exposes input: %v", err)
		}
	}
}

func TestErrorsDoNotExposeResponseBody(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusUnprocessableEntity, http.StatusInternalServerError} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				fmt.Fprint(w, `{"message":"private-token secret-value"}`)
			})
			_, err := client.ListProjects(context.Background())
			var responseError *HTTPError
			if !errors.As(err, &responseError) || responseError.StatusCode != status {
				t.Fatalf("error = %v, want HTTP %d", err, status)
			}
			if strings.Contains(err.Error(), "private-token") || strings.Contains(err.Error(), "secret-value") {
				t.Fatalf("error exposes body: %v", err)
			}
		})
	}
}

func TestGETRetriesAndBoundedRetryAfter(t *testing.T) {
	var attempts atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) <= 2 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, `[]`)
	}, WithRetries(2), WithRetryDelay(0))
	if _, err := client.ListProjects(context.Background()); err != nil || attempts.Load() != 3 {
		t.Fatalf("attempts = %d, error = %v", attempts.Load(), err)
	}
	for _, header := range []string{"1", time.Now().Add(10 * time.Second).UTC().Format(http.TimeFormat)} {
		if delay, retry := client.retryAfter(header, 0); !retry || delay < time.Second || delay > 10*time.Second {
			t.Errorf("Retry-After %q = %s, %v", header, delay, retry)
		}
	}
	attempts.Store(0)
	client = newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
	}, WithRetries(5), WithRetryDelay(0))
	_, err := client.ListProjects(context.Background())
	var responseError *HTTPError
	if !errors.As(err, &responseError) || responseError.StatusCode != 429 || attempts.Load() != 1 {
		t.Fatalf("long backoff: attempts = %d, error = %v", attempts.Load(), err)
	}
}

func TestCancellationDuringGETAndRetryWait(t *testing.T) {
	t.Run("request", func(t *testing.T) {
		started := make(chan struct{})
		client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			close(started)
			<-r.Context().Done()
		})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() { _, err := client.ListProjects(ctx); done <- err }()
		<-started
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want cancellation", err)
		}
	})
	t.Run("retry wait", func(t *testing.T) {
		var attempts atomic.Int32
		client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			attempts.Add(1)
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(http.StatusServiceUnavailable)
		}, WithRetries(2))
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		_, err := client.ListProjects(ctx)
		if !errors.Is(err, context.DeadlineExceeded) || attempts.Load() != 1 {
			t.Fatalf("attempts = %d, error = %v", attempts.Load(), err)
		}
	})
}

func TestPOSTIsNeverReplayed(t *testing.T) {
	for _, failure := range []string{"server", "disconnect", "malformed", "missing deployments", "timeout"} {
		t.Run(failure, func(t *testing.T) {
			var attempts atomic.Int32
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				attempts.Add(1)
				switch failure {
				case "server":
					w.WriteHeader(http.StatusInternalServerError)
					fmt.Fprint(w, `{"message":"secret"}`)
				case "disconnect":
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					conn.Close()
				case "malformed":
					fmt.Fprint(w, `{"secret":`)
				case "missing deployments":
					fmt.Fprint(w, `{"message":"secret"}`)
				case "timeout":
					io.Copy(io.Discard, r.Body)
					<-r.Context().Done()
				}
			}, WithRetries(5), WithRetryDelay(0))
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, err := client.Deploy(ctx, models.DeployRequest{ApplicationUUID: "a1", Force: false})
			var uncertain *UncertainSubmissionError
			if !errors.As(err, &uncertain) || attempts.Load() != 1 || uncertain.ResourceUUID != "a1" {
				t.Fatalf("attempts = %d, error = %v", attempts.Load(), err)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Errorf("error exposes response body: %v", err)
			}
		})
	}
}

func TestPOSTKnownRejectionAndCancelledBeforeSend(t *testing.T) {
	var attempts atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}, WithRetries(5))
	_, err := client.Deploy(context.Background(), models.DeployRequest{ApplicationUUID: "a1", Force: false})
	var responseError *HTTPError
	var uncertain *UncertainSubmissionError
	if !errors.As(err, &responseError) || errors.As(err, &uncertain) || attempts.Load() != 1 {
		t.Fatalf("known rejection: attempts = %d, error = %v", attempts.Load(), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.Deploy(ctx, models.DeployRequest{ApplicationUUID: "a1", Force: false})
	if !errors.Is(err, context.Canceled) || errors.As(err, &uncertain) || attempts.Load() != 1 {
		t.Fatalf("before-send cancellation: attempts = %d, error = %v", attempts.Load(), err)
	}
	for _, uuid := range []string{"", "a1,a2", "a1 a2", "a1\n"} {
		if _, err := client.Deploy(context.Background(), models.DeployRequest{ApplicationUUID: uuid, Force: false}); err == nil || attempts.Load() != 1 {
			t.Fatalf("invalid deployment UUID %q accepted", uuid)
		}
	}
}

func TestRedirectCannotForwardBearerToken(t *testing.T) {
	var destinationCalls atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		destinationCalls.Add(1)
		t.Errorf("redirect destination received request with authorization %q", r.Header.Get("Authorization"))
	}))
	defer destination.Close()
	provided := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { t.Error("supplied redirect callback called"); return nil }}
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}, WithHTTPClient(provided))
	_, err := client.ListProjects(context.Background())
	var responseError *HTTPError
	if !errors.As(err, &responseError) || responseError.StatusCode != http.StatusTemporaryRedirect || destinationCalls.Load() != 0 {
		t.Fatalf("redirect: error = %v, destination calls = %d", err, destinationCalls.Load())
	}
	if client.http == provided {
		t.Error("client did not copy injected HTTP client")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTransportErrorsAreSanitizedAndUnwrapped(t *testing.T) {
	cause := errors.New("private-token secret-value")
	client, err := NewClient("https://example.test", "private-token", WithRetries(0), WithHTTPClient(&http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, cause }),
	}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ListProjects(context.Background())
	if !errors.Is(err, cause) || strings.Contains(err.Error(), "private-token") || strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("transport error = %v", err)
	}
}

func TestRejectIncompleteOrInvalidResponses(t *testing.T) {
	for _, body := range []string{`null`, `{}`, `{"data":[]}`, `[] []`, `{"secret":`} {
		t.Run(body, func(t *testing.T) {
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
			_, err := client.ListProjects(context.Background())
			var protocolError *ProtocolError
			if !errors.As(err, &protocolError) || strings.Contains(err.Error(), "secret") {
				t.Fatalf("error = %v", err)
			}
		})
	}
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", `</api/v1/projects?page=2>; rel="next"`)
		fmt.Fprint(w, `[{"uuid":"p1","name":"Personal"}]`)
	})
	if _, err := client.ListProjects(context.Background()); err == nil {
		t.Error("accepted incomplete paginated list")
	}
	for _, body := range []string{`{}`, `{"logs":null}`, `{"message":"secret"}`} {
		client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
		if _, err := client.Logs(context.Background(), "a1", 100); err == nil || strings.Contains(err.Error(), "secret") {
			t.Errorf("missing logs error = %v", err)
		}
	}
	client = newTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"logs":""}`) })
	if logs, err := client.Logs(context.Background(), "a1", 100); err != nil || logs.Logs != "" {
		t.Errorf("explicit empty logs = %#v, %v", logs, err)
	}
}
