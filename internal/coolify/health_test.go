package coolify

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthAsksThePublicRouteWithoutTheToken(t *testing.T) {
	var seen *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r
		if r.URL.Path != "/coolify/api/health" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("OK"))
	}))
	defer server.Close()
	if err := CheckHealth(context.Background(), server.URL+"/coolify/"); err != nil {
		t.Fatal(err)
	}
	if seen.Header.Get("Authorization") != "" || seen.Method != http.MethodGet || !strings.HasPrefix(seen.Header.Get("User-Agent"), "coolship/") {
		t.Fatalf("request: %s %v", seen.Method, seen.Header)
	}
}

func TestHealthNamesEveryWayAURLCanBeWrong(t *testing.T) {
	answer := func(status int, body string, header ...string) string {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if len(header) == 2 {
				w.Header().Set(header[0], header[1])
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
		t.Cleanup(server.Close)
		return server.URL
	}
	notCoolify := func(err error) bool { var target *NotCoolifyError; return errors.As(err, &target) }
	httpStatus := func(code int) func(error) bool {
		return func(err error) bool {
			var target *HTTPError
			return errors.As(err, &target) && target.StatusCode == code
		}
	}
	transport := func(kind TransportKind) func(error) bool {
		return func(err error) bool {
			var target *RequestError
			return errors.As(err, &target) && target.Kind() == kind
		}
	}
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("OK")) }))
	defer tlsServer.Close()
	plain := answer(http.StatusOK, "OK")
	for _, test := range []struct {
		name string
		url  string
		opts []Option
		want func(error) bool
		text string
	}{
		{"another site", answer(http.StatusNotFound, "<html>"), nil, notCoolify, "HTTP 404 Not Found"},
		{"a page that is not the health check", answer(http.StatusOK, "<html>welcome</html>"), nil, notCoolify, "the answer is not Coolify's"},
		{"a redirect", answer(http.StatusFound, "", "Location", "https://elsewhere.example.com/"), nil, httpStatus(http.StatusFound), "HTTP 302"},
		{"a server fault", answer(http.StatusBadGateway, ""), nil, httpStatus(http.StatusBadGateway), "HTTP 502"},
		{"an untrusted certificate", tlsServer.URL, nil, transport(TransportCertificate), "TLS certificate"},
		{"https to a plain server", strings.Replace(plain, "http://", "https://", 1), nil, transport(TransportNotTLS), "did not answer with TLS"},
		{"an unknown host", "https://coolify.invalid", []Option{WithHTTPClient(&http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) {
			return nil, &net.DNSError{Err: "no such host", Name: "coolify.invalid", IsNotFound: true}
		})})}, transport(TransportHostNotFound), "host name could not be resolved"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := CheckHealth(context.Background(), test.url, test.opts...)
			if err == nil || !test.want(err) || !strings.Contains(err.Error(), test.text) || !strings.Contains(err.Error(), "/api/health") {
				t.Fatalf("err = %v (%T)", err, err)
			}
		})
	}
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
