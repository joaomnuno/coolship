package ui

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestTraceWritesNothingAtNormalAndIsNilSafe(t *testing.T) {
	var nilTrace *Trace
	nilTrace.SetLevel(VerbosityDebug)
	nilTrace.Exchange(Exchange{Method: "GET"})
	if nilTrace.Level() != VerbosityNormal {
		t.Fatal("a nil trace must be normal")
	}
	var out bytes.Buffer
	trace := NewTrace(&out)
	trace.Exchange(Exchange{Method: "GET", URL: "https://c.example/api/v1/version", Status: 200})
	if out.Len() != 0 {
		t.Fatalf("normal wrote %q", out.String())
	}
}

func TestTraceVerboseIsOneLinePerAttempt(t *testing.T) {
	var out bytes.Buffer
	trace := NewTrace(&out)
	trace.SetLevel(VerbosityVerbose)
	trace.Exchange(Exchange{Method: "GET", URL: "https://c.example/api/v1/applications/a", Status: 502, Duration: 1234567 * time.Nanosecond,
		RequestHeader: http.Header{"Authorization": {"Bearer ****abcd"}}, ResponseBody: []byte("oops")})
	trace.Exchange(Exchange{Method: "GET", URL: "https://c.example/api/v1/applications/a", Attempt: 1, Duration: 3 * time.Millisecond, Err: errors.New("dial tcp secret-detail")})
	want := "GET https://c.example/api/v1/applications/a 502 Bad Gateway 1ms\n" +
		"GET https://c.example/api/v1/applications/a no response 3ms (retry 1)\n"
	if out.String() != want {
		t.Fatalf("got %q\nwant %q", out.String(), want)
	}
}

func TestTraceDebugShowsHeadersAndBodiesWithoutControlCharacters(t *testing.T) {
	var out bytes.Buffer
	trace := NewTrace(&out)
	trace.SetLevel(VerbosityDebug)
	trace.Exchange(Exchange{
		Method: "POST", URL: "https://c.example/api/v1/deploy", Status: 200, Duration: 20 * time.Millisecond,
		RequestHeader:  http.Header{"Authorization": {"Bearer ****abcd"}, "Accept": {"application/json"}},
		RequestBody:    []byte(`{"uuid":"a"}`),
		ResponseHeader: http.Header{"Content-Type": {"application/json"}},
		ResponseBody:   []byte("{\"deployments\":[]}\n\x1b[2Jline\ttwo\n"),
	})
	want := "POST https://c.example/api/v1/deploy 200 OK 20ms\n" +
		"> Accept: application/json\n" +
		"> Authorization: Bearer ****abcd\n" +
		">\n" +
		"> {\"uuid\":\"a\"}\n" +
		"< Content-Type: application/json\n" +
		"<\n" +
		"< {\"deployments\":[]}\n" +
		"< [2Jline\ttwo\n"
	if out.String() != want {
		t.Fatalf("got %q\nwant %q", out.String(), want)
	}
	if strings.Contains(out.String(), "\x1b") {
		t.Fatal("escape sequence reached the terminal")
	}
}

// TestLiveViewsAreOffAboveNormal checks the verbosity gate of drawable on
// its own, since a test has no terminal to reach the rest of it.
func TestLiveViewsAreOffAboveNormal(t *testing.T) {
	for _, test := range []struct {
		name        string
		interactive bool
		trace       *Trace
		want        bool
	}{
		{"nil trace is normal", true, nil, true},
		{"normal", true, NewTrace(nil), true},
		{"verbose", true, leveled(VerbosityVerbose), false},
		{"debug", true, leveled(VerbosityDebug), false},
		{"not interactive", false, NewTrace(nil), false},
	} {
		if got := liveViewsAllowed(Streams{Interactive: test.interactive, Trace: test.trace}); got != test.want {
			t.Errorf("%s: liveViewsAllowed = %v, want %v", test.name, got, test.want)
		}
	}
}

func leveled(level Verbosity) *Trace {
	trace := NewTrace(nil)
	trace.SetLevel(level)
	return trace
}
