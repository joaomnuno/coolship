package coolify

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

// TestTraceReportsEveryAttemptWithTheTokenMasked checks the trace hook: one
// Exchange per attempt, retries counted, request and response bodies kept,
// and the bearer token reduced to its last four characters everywhere.
func TestTraceReportsEveryAttemptWithTheTokenMasked(t *testing.T) {
	var calls atomic.Int32
	var exchanges []Exchange
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"message":"busy"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"version":"4.3.18"}`)
	}, WithRetries(1), WithRetryDelay(0), WithTrace(func(e Exchange) { exchanges = append(exchanges, e) }))
	data, _, err := client.fetch(context.Background(), http.MethodGet, []string{"version"}, nil, nil)
	if err != nil || !strings.Contains(string(data), "4.3.18") {
		t.Fatalf("fetch: %q %v", data, err)
	}
	if len(exchanges) != 2 {
		t.Fatalf("got %d exchanges, want 2", len(exchanges))
	}
	first, second := exchanges[0], exchanges[1]
	if first.Status != 503 || first.Attempt != 0 || string(first.ResponseBody) != `{"message":"busy"}` || first.Method != "GET" || !strings.HasSuffix(first.URL, "/api/v1/version") {
		t.Fatalf("first: %+v", first)
	}
	if second.Status != 200 || second.Attempt != 1 || string(second.ResponseBody) != `{"version":"4.3.18"}` || second.ResponseHeader.Get("Content-Type") != "application/json" {
		t.Fatalf("second: %+v", second)
	}
	for _, e := range exchanges {
		if got := e.RequestHeader.Get("Authorization"); got != "Bearer ****oken" {
			t.Fatalf("authorization = %q", got)
		}
		if strings.Contains(fmt.Sprintf("%+v", e), "fixture-token") {
			t.Fatal("the token reached the trace")
		}
	}
}

// TestTraceKeepsARefusedMutationsExplanation checks that reading the refusal
// body for the trace leaves the error's server message intact.
func TestTraceKeepsARefusedMutationsExplanation(t *testing.T) {
	var traced Exchange
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, `{"message":"bad branch"}`)
	}, WithTrace(func(e Exchange) { traced = e }))
	_, _, err := client.fetch(context.Background(), http.MethodPost, []string{"deploy"}, nil, map[string]string{"uuid": "a"})
	if err == nil || !strings.Contains(err.Error(), "bad branch") {
		t.Fatalf("error lost the explanation: %v", err)
	}
	if traced.Status != 422 || string(traced.RequestBody) != `{"uuid":"a"}` || string(traced.ResponseBody) != `{"message":"bad branch"}` {
		t.Fatalf("traced: %+v", traced)
	}
}

func TestMaskTokenKeepsTheLastFour(t *testing.T) {
	for token, want := range map[string]string{"1|abcdefgh": "****efgh", "abcd": "****", "ab": "**"} {
		if got := maskToken(token); got != want {
			t.Errorf("maskToken(%q) = %q, want %q", token, got, want)
		}
	}
}
