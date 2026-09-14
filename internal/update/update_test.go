package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestEnabledOnlyForAPersonOnAReleasedBuild(t *testing.T) {
	off, on := false, true
	env := func(values map[string]string) func(string) string {
		return func(key string) string { return values[key] }
	}
	base := Conditions{Version: "v0.3.0", StderrTerminal: true, Env: env(nil)}
	for _, test := range []struct {
		name   string
		change func(*Conditions)
		want   bool
	}{
		{"released build at a terminal", func(*Conditions) {}, true},
		{"release candidate", func(c *Conditions) { c.Version = "v0.4.0-rc.1" }, true},
		{"nightly", func(c *Conditions) { c.Version = "v0.3.1-nightly.abc1234" }, true},
		{"preference on", func(c *Conditions) { c.Preference = &on }, true},
		{"stderr not a terminal", func(c *Conditions) { c.StderrTerminal = false }, false},
		{"json output", func(c *Conditions) { c.JSON = true }, false},
		{"CI", func(c *Conditions) { c.Env = env(map[string]string{"CI": "true"}) }, false},
		{"opt-out variable", func(c *Conditions) { c.Env = env(map[string]string{EnvDisable: "1"}) }, false},
		{"preference off", func(c *Conditions) { c.Preference = &off }, false},
		{"dev build", func(c *Conditions) { c.Version = "dev" }, false},
		{"dev build with revision", func(c *Conditions) { c.Version = "dev (abc123456789)" }, false},
		{"no version", func(c *Conditions) { c.Version = "" }, false},
		{"described past a tag", func(c *Conditions) { c.Version = "v0.3.0-5-gabc1234" }, false},
		{"dirty tag", func(c *Conditions) { c.Version = "v0.3.0-dirty" }, false},
		{"pseudo-version", func(c *Conditions) { c.Version = "v0.3.1-0.20260901120000-abcdef123456" }, false},
		{"nil environment", func(c *Conditions) { c.Env = nil }, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			conditions := base
			test.change(&conditions)
			if got := Enabled(conditions); got != test.want {
				t.Errorf("Enabled = %v, want %v", got, test.want)
			}
		})
	}
}

func TestVersionOrdering(t *testing.T) {
	ordered := []string{"v0.2.9", "v0.3.0-alpha", "v0.3.0-alpha.1", "v0.3.0-alpha.beta", "v0.3.0-rc.1", "v0.3.0-rc.2", "v0.3.0-rc.10", "0.3.0", "v0.3.1", "v0.10.0", "v1.0.0"}
	for i := 0; i+1 < len(ordered); i++ {
		a, okA := parseVersion(ordered[i])
		b, okB := parseVersion(ordered[i+1])
		if !okA || !okB || compareVersions(a, b) != -1 || compareVersions(b, a) != 1 {
			t.Errorf("%s < %s not established (%v %v)", ordered[i], ordered[i+1], okA, okB)
		}
	}
	a, _ := parseVersion("v0.3.0")
	b, _ := parseVersion("0.3.0")
	if compareVersions(a, b) != 0 {
		t.Error("the v prefix changed the order")
	}
	for _, bad := range []string{"", "v1", "v1.2", "v1.2.3.4", "v1.2.x", "v1.2.3-", "v1.2.3-a..b", "v1.2.3+build", "v-1.2.3"} {
		if _, ok := parseVersion(bad); ok {
			t.Errorf("parseVersion(%q) accepted", bad)
		}
	}
}

// releases is a fake GitHub releases endpoint that counts its requests.
type releases struct {
	server   *httptest.Server
	requests atomic.Int32
}

func newReleases(t *testing.T, handler func(http.ResponseWriter, *http.Request)) *releases {
	t.Helper()
	r := &releases{}
	r.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.requests.Add(1)
		handler(w, req)
	}))
	t.Cleanup(r.server.Close)
	return r
}

func release(tag string, prerelease bool) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name":%q,"draft":false,"prerelease":%v,"name":"ignored"}`, tag, prerelease)
	}
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func notifier(path, url, current string, c *clock) *Notifier {
	return &Notifier{Current: current, StatePath: path, URL: url, UserAgent: "coolship/" + current, Now: c.Now}
}

// run is one command: start, let the check finish as a slow command would,
// then finish.
func run(t *testing.T, n *Notifier) string {
	t.Helper()
	n.Start(context.Background())
	if n.done != nil {
		select {
		case <-n.done:
		case <-time.After(5 * time.Second):
			t.Fatal("check did not finish")
		}
	}
	return n.Finish(true)
}

func TestNoticeOncePerDayPerRelease(t *testing.T) {
	var userAgent atomic.Value
	server := newReleases(t, func(w http.ResponseWriter, r *http.Request) {
		userAgent.Store(r.Header.Get("User-Agent"))
		release("v0.4.0", false)(w, r)
	})
	path := filepath.Join(t.TempDir(), "coolship", StateFile)
	c := &clock{now: time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)}

	notice := run(t, notifier(path, server.server.URL, "v0.3.0", c))
	want := "A new Coolship release is available: v0.4.0 (you have v0.3.0). Upgrade: " + InstallCommand
	if notice != want {
		t.Fatalf("notice = %q, want %q", notice, want)
	}
	if userAgent.Load() != "coolship/v0.3.0" {
		t.Errorf("User-Agent = %v", userAgent.Load())
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("state file = %v, %v", info, err)
	}

	// An hour later: no request, and no second notice.
	c.now = c.now.Add(time.Hour)
	if notice := run(t, notifier(path, server.server.URL, "v0.3.0", c)); notice != "" || server.requests.Load() != 1 {
		t.Fatalf("same day: notice = %q, requests = %d", notice, server.requests.Load())
	}
	// A day later: checked again, and reminded.
	c.now = c.now.Add(Interval)
	if notice := run(t, notifier(path, server.server.URL, "v0.3.0", c)); notice != want || server.requests.Load() != 2 {
		t.Fatalf("next day: notice = %q, requests = %d", notice, server.requests.Load())
	}
	// Once upgraded, nothing.
	c.now = c.now.Add(2 * Interval)
	if notice := run(t, notifier(path, server.server.URL, "v0.4.0", c)); notice != "" {
		t.Fatalf("up to date: notice = %q", notice)
	}
}

func TestANewerReleaseIsAnnouncedTheSameDay(t *testing.T) {
	path := filepath.Join(t.TempDir(), StateFile)
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	if err := writeState(path, State{CheckedAt: now.Add(-25 * time.Hour), LatestVersion: "v0.4.0", NotifiedAt: now.Add(-time.Hour), NotifiedVersion: "v0.4.0"}); err != nil {
		t.Fatal(err)
	}
	server := newReleases(t, release("v0.4.1", false))
	if notice := run(t, notifier(path, server.server.URL, "v0.3.0", &clock{now})); !strings.Contains(notice, "v0.4.1 (you have v0.3.0)") {
		t.Fatalf("notice = %q", notice)
	}
}

func TestPreReleasesDraftsAndBadAnswersAreNotReleases(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{"prerelease flag", release("v0.5.0", true)},
		{"prerelease tag", release("v0.5.0-rc.1", false)},
		{"draft", func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, `{"tag_name":"v0.5.0","draft":true}`)
		}},
		{"not a version", release("nightly", false)},
		{"not JSON", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "<html>") }},
		{"rate limited", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), StateFile)
			server := newReleases(t, test.handler)
			if notice := run(t, notifier(path, server.server.URL, "v0.3.0", &clock{now})); notice != "" {
				t.Fatalf("notice = %q", notice)
			}
			// Any answer counts as the day's check.
			state, err := readState(path)
			if err != nil || !state.CheckedAt.Equal(now) || state.LatestVersion != "" {
				t.Fatalf("state = %+v, %v", state, err)
			}
		})
	}
}

func TestFinishAbandonsASlowCheckWithoutRecordingIt(t *testing.T) {
	release := make(chan struct{})
	server := newReleases(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	defer close(release)
	path := filepath.Join(t.TempDir(), StateFile)
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	// A cached answer from an earlier run is still announced.
	if err := writeState(path, State{CheckedAt: now.Add(-2 * Interval), LatestVersion: "v0.4.0"}); err != nil {
		t.Fatal(err)
	}
	n := notifier(path, server.server.URL, "v0.3.0", &clock{now})
	n.Start(context.Background())
	deadline := time.Now().Add(5 * time.Second)
	for server.requests.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	started := time.Now()
	notice := n.Finish(true)
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Errorf("Finish waited %v for the check", elapsed)
	}
	if !strings.Contains(notice, "v0.4.0") {
		t.Errorf("notice = %q, want the cached release", notice)
	}
	state, _ := readState(path)
	if !state.CheckedAt.Equal(now.Add(-2 * Interval)) {
		t.Errorf("abandoned check was recorded: %+v", state)
	}
}

func TestUnreachableServerAndDamagedCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), StateFile)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.NotFoundHandler())
	url := server.URL
	server.Close()
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	if notice := run(t, notifier(path, url, "v0.3.0", &clock{now})); notice != "" {
		t.Fatalf("notice = %q", notice)
	}
	data, _ := os.ReadFile(path)
	var state State
	if json.Unmarshal(data, &state) == nil && !state.CheckedAt.IsZero() {
		t.Errorf("a failed check was recorded: %s", data)
	}
	// A future timestamp from a changed clock does not suppress checks.
	if err := writeState(path, State{CheckedAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	counted := newReleases(t, release("v0.3.0", false))
	run(t, notifier(path, counted.server.URL, "v0.3.0", &clock{now}))
	if counted.requests.Load() != 1 {
		t.Errorf("requests = %d, want a check despite the future timestamp", counted.requests.Load())
	}
}

func TestAnInterruptedRunDoesNotUseUpTheNotice(t *testing.T) {
	path := filepath.Join(t.TempDir(), StateFile)
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	if err := writeState(path, State{CheckedAt: now, LatestVersion: "v0.4.0"}); err != nil {
		t.Fatal(err)
	}
	if notice := notifier(path, "", "v0.3.0", &clock{now}).Finish(false); notice != "" {
		t.Fatalf("hidden notice = %q", notice)
	}
	if state, _ := readState(path); !state.NotifiedAt.IsZero() || state.NotifiedVersion != "" {
		t.Fatalf("an unseen notice was recorded: %+v", state)
	}
	if notice := notifier(path, "", "v0.3.0", &clock{now}).Finish(true); !strings.Contains(notice, "v0.4.0") {
		t.Fatalf("next run notice = %q", notice)
	}
}
