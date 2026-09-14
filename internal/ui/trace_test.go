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

func TestTraceVerboseFoldsRepeatedPolls(t *testing.T) {
	var out bytes.Buffer
	trace := NewTrace(&out)
	trace.SetLevel(VerbosityVerbose)
	poll := Exchange{Method: "GET", URL: "https://c.example/api/v1/deployments/d", Status: 200, Duration: 40 * time.Millisecond}
	for i := range 12 {
		poll.Duration = time.Duration(40+i) * time.Millisecond
		trace.Exchange(poll)
	}
	if want := "GET https://c.example/api/v1/deployments/d 200 OK 40ms\n"; out.String() != want {
		t.Fatalf("while repeating got %q, want %q", out.String(), want)
	}
	// A different exchange writes the summary first; a retry and a failure
	// are never folded, even when identical.
	trace.Exchange(Exchange{Method: "GET", URL: "https://c.example/api/v1/applications/a", Status: 200, Duration: time.Millisecond})
	retry := Exchange{Method: "GET", URL: "https://c.example/api/v1/applications/a", Status: 502, Attempt: 1, Duration: time.Millisecond}
	trace.Exchange(retry)
	trace.Exchange(retry)
	failed := Exchange{Method: "GET", URL: "https://c.example/api/v1/applications/a", Duration: time.Millisecond, Err: errors.New("dial")}
	trace.Exchange(failed)
	trace.Exchange(failed)
	trace.Exchange(poll)
	trace.Exchange(poll)
	trace.Flush()
	trace.Flush()
	want := "GET https://c.example/api/v1/deployments/d 200 OK 40ms\n" +
		"GET https://c.example/api/v1/deployments/d 200 OK ×11 more (last 51ms)\n" +
		"GET https://c.example/api/v1/applications/a 200 OK 1ms\n" +
		"GET https://c.example/api/v1/applications/a 502 Bad Gateway 1ms (retry 1)\n" +
		"GET https://c.example/api/v1/applications/a 502 Bad Gateway 1ms (retry 1)\n" +
		"GET https://c.example/api/v1/applications/a no response 1ms\n" +
		"GET https://c.example/api/v1/applications/a no response 1ms\n" +
		"GET https://c.example/api/v1/deployments/d 200 OK 51ms\n" +
		"GET https://c.example/api/v1/deployments/d 200 OK ×1 more (last 51ms)\n"
	if out.String() != want {
		t.Fatalf("got %q\nwant %q", out.String(), want)
	}
	// After a flush the same exchange starts a new run.
	out.Reset()
	trace.Exchange(poll)
	if out.String() != "GET https://c.example/api/v1/deployments/d 200 OK 51ms\n" {
		t.Fatalf("after flush got %q", out.String())
	}
}

func TestTraceDebugKeepsEveryExchange(t *testing.T) {
	var out bytes.Buffer
	trace := NewTrace(&out)
	trace.SetLevel(VerbosityDebug)
	poll := Exchange{Method: "GET", URL: "https://c.example/api/v1/deployments/d", Status: 200, Duration: time.Millisecond}
	trace.Exchange(poll)
	trace.Exchange(poll)
	trace.Flush()
	var nilTrace *Trace
	nilTrace.Flush()
	if got := strings.Count(out.String(), "GET https://c.example/api/v1/deployments/d 200 OK 1ms\n"); got != 2 || strings.Contains(out.String(), "more") {
		t.Fatalf("debug got %q", out.String())
	}
}
