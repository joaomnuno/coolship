package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/joaomnuno/coolship/internal/auth"
	"github.com/joaomnuno/coolship/internal/coolify"
	"github.com/joaomnuno/coolship/internal/service"
)

var (
	flowShiftTab = tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	flowUp       = tea.KeyPressMsg{Code: tea.KeyUp}
	flowClear    = tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}
)

func typeKeys(text string) []tea.Msg {
	msgs := make([]tea.Msg, 0, len(text))
	for _, r := range text {
		msgs = append(msgs, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return msgs
}

// keys joins key messages and typed text into one script.
func keys(parts ...any) []tea.Msg {
	var msgs []tea.Msg
	for _, part := range parts {
		switch part := part.(type) {
		case string:
			msgs = append(msgs, typeKeys(part)...)
		case tea.Msg:
			msgs = append(msgs, part)
		}
	}
	return msgs
}

// driveFlow feeds messages to a flow the way its program would, running the
// commands each update returns. A command that does not answer promptly is a
// timer (the spinner, a cursor blink) and is dropped.
func driveFlow(t *testing.T, m *flowModel, msgs ...tea.Msg) {
	t.Helper()
	for _, msg := range msgs {
		_, cmd := m.Update(msg)
		runFlowCmd(m, cmd, 0)
	}
}

func runFlowCmd(m *flowModel, cmd tea.Cmd, depth int) {
	var run func(tea.Cmd, int)
	run = func(cmd tea.Cmd, depth int) {
		if cmd == nil || depth > 50 {
			return
		}
		result := make(chan tea.Msg, 1)
		go func() { result <- cmd() }()
		select {
		case msg := <-result:
			switch msg := msg.(type) {
			case nil, tea.QuitMsg:
			case tea.BatchMsg:
				for _, next := range msg {
					run(next, depth+1)
				}
			default:
				_, next := m.Update(msg)
				run(next, depth+1)
			}
		case <-time.After(20 * time.Millisecond):
		}
	}
	run(cmd, depth)
}

// coolifyForLogin answers the requests a login makes: the public health
// check, and the version and team for the tokens it accepts. refuse, when
// set, answers the version request instead.
func coolifyForLogin(t *testing.T, tokens []string, refuse func(http.ResponseWriter) bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("OK")) })
	authorized := func(r *http.Request) bool {
		for _, token := range tokens {
			if r.Header.Get("Authorization") == "Bearer "+token {
				return true
			}
		}
		return false
	}
	mux.HandleFunc("GET /api/v1/version", func(w http.ResponseWriter, r *http.Request) {
		if refuse != nil && refuse(w) {
			return
		}
		if !authorized(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte("4.3.18"))
	})
	mux.HandleFunc("GET /api/v1/teams/current", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"id":0,"name":"Platform"}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// loginApp is the real service against the test server, writing to a
// temporary credentials file. health, when set, replaces the health check.
func loginApp(health func(context.Context, string) error) *service.App {
	if health == nil {
		health = func(ctx context.Context, url string) error { return coolify.CheckHealth(ctx, url) }
	}
	return service.New(service.Dependencies{
		NewBackend: func(c auth.Credentials) (service.Backend, error) {
			return coolify.NewClient(c.URL, c.Token, coolify.WithRetries(0))
		},
		CheckHealth: health,
	})
}

func newTestLoginFlow(actions LoginActions, preset service.LoginOptions, open func(string) error) (*flowModel, *loginForm) {
	form := newLoginForm(actions, preset)
	steps := NewSteps(Streams{}, "human", loginTitles)
	// Wide enough that no explanation wraps inside a phrase a test looks for.
	m := newFlowModel(context.Background(), steps, form.stages(), newPalette(false), 240, open)
	m.clock = func() time.Time { return time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC) }
	// Checks run inside the update, so a script sees their outcome at once.
	m.launch = func(work func() tea.Msg) tea.Cmd {
		msg := work()
		return func() tea.Msg { return msg }
	}
	return m, form
}

func startFlow(t *testing.T, m *flowModel) {
	t.Helper()
	runFlowCmd(m, m.Init(), 0)
}

// mustContain checks the view as it reads, without the escapes the text
// field draws its cursor and prompt with.
func mustContain(t *testing.T, view string, parts ...string) {
	t.Helper()
	view = ansi.Strip(view)
	for _, part := range parts {
		if !strings.Contains(view, part) {
			t.Fatalf("view lacks %q:\n%s", part, view)
		}
	}
}

func TestLoginFormAsksStepByStepAndSavesOnlyAtTheEnd(t *testing.T) {
	server := coolifyForLogin(t, []string{"token-1234abcd"}, nil)
	path := filepath.Join(t.TempDir(), "config.json")
	m, form := newTestLoginFlow(loginApp(nil), service.LoginOptions{ConfigPath: path}, nil)
	startFlow(t, m)
	mustContain(t, m.render(), "? Where does Coolify run?", "❯ Self-hosted", "Coolify Cloud  https://app.coolify.io", "  URL", "  Save", "esc leave")
	if strings.Contains(m.render(), "shift+tab") {
		t.Fatal("the first question has nothing to go back to")
	}
	// Up at the top of the first list goes nowhere.
	driveFlow(t, m, flowUp)
	driveFlow(t, m, keyEnter)
	mustContain(t, m.render(), "✓ Instance type", "Self-hosted", "? Coolify URL", "shift+tab edit previous · esc leave")

	// A URL that is not one is refused where it was typed.
	driveFlow(t, m, keys("coolify.example.com", keyEnter)...)
	mustContain(t, m.render(), "Enter the full URL, such as https://coolify.example.com")
	driveFlow(t, m, keys(flowClear, server.URL, keyEnter)...)
	view := m.render()
	mustContain(t, view, "✓ URL", server.URL, "✓ Check instance", "Coolify answered", "? Context name", "> 127")

	// Shift+Tab returns to the URL with the answer kept, and clears the rows after it.
	driveFlow(t, m, flowShiftTab)
	view = m.render()
	mustContain(t, view, "? Coolify URL", "> "+server.URL, "  Check instance")
	if strings.Contains(view, "✓ Check instance") {
		t.Fatalf("going back kept a later row:\n%s", view)
	}
	// Up in a field with text in it stays; in an empty one it goes back too.
	driveFlow(t, m, flowUp)
	mustContain(t, m.render(), "? Coolify URL")
	driveFlow(t, m, flowClear, flowUp)
	mustContain(t, m.render(), "? Where does Coolify run?")
	driveFlow(t, m, keyEnter)
	mustContain(t, m.render(), "> "+server.URL)
	driveFlow(t, m, keys(keyEnter, flowClear, "lab", keyEnter)...)
	mustContain(t, m.render(), "✓ Context name", "lab", "? API token", server.URL+"/security/api-tokens")
	if strings.Contains(m.render(), "write") || strings.Contains(m.render(), "deploy") {
		t.Fatalf("the token question names abilities:\n%s", m.render())
	}

	// The token is never echoed; the row shows its last four characters.
	driveFlow(t, m, keys("token-1234abcd")...)
	if strings.Contains(m.render(), "token-1234abcd") {
		t.Fatal("token echoed")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("something was written before the last step")
	}
	driveFlow(t, m, keyEnter)
	if !m.finished || m.err != nil {
		t.Fatalf("finished=%v err=%v view:\n%s", m.finished, m.err, m.render())
	}
	final := m.steps.rows
	if final[loginToken].detail != "••••abcd" || final[loginCheckToken].detail != "team Platform on Coolify 4.3.18" || final[loginSave].detail != path {
		t.Fatalf("rows: %+v", final)
	}
	result := form.result
	if result.Name != "lab" || result.URL != server.URL || result.Team != "Platform" || !result.Default {
		t.Fatalf("result: %+v", result)
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), `"token": "token-1234abcd"`) {
		t.Fatalf("saved: %s", data)
	}
}

func TestLoginFormExplainsAFailedCheckAndOffersRetryGoBackLeave(t *testing.T) {
	server := coolifyForLogin(t, []string{"right-token-0001"}, nil)
	path := filepath.Join(t.TempDir(), "config.json")
	m, _ := newTestLoginFlow(loginApp(nil), service.LoginOptions{ConfigPath: path, URL: server.URL, Name: "lab"}, nil)
	startFlow(t, m)
	// Flags answer their questions; the instance is checked straight away.
	mustContain(t, m.render(), "✓ Instance type", "✓ URL", "✓ Check instance", "✓ Context name", "? API token")
	driveFlow(t, m, keys("wrong-token-0002", keyEnter)...)
	view := m.render()
	mustContain(t, view, "✗ Check token", "Error [unauthorized]: could not verify "+server.URL+": Coolify GET /version: HTTP 401 Unauthorized",
		"Hint: The server rejected the token", "Docs: https://coolship.itrocas.com/docs/platform/errors#unauthorized",
		"❯ Retry", "  Go back", "  Leave")
	// Retry asks again with the same token and fails the same way.
	driveFlow(t, m, keyEnter)
	mustContain(t, m.render(), "✗ Check token", "❯ Retry")
	// Go back returns to the token, whose answer is kept for editing.
	driveFlow(t, m, keyDown, keyEnter)
	mustContain(t, m.render(), "? API token", "> ••••••••••••••••")
	driveFlow(t, m, keys(flowClear, "right-token-0001", keyEnter)...)
	if !m.finished || m.err != nil {
		t.Fatalf("finished=%v err=%v:\n%s", m.finished, m.err, m.render())
	}
}

func TestLoginFormOffersTheCoolifyPageForASetupProblem(t *testing.T) {
	server := coolifyForLogin(t, []string{"token-1234abcd"}, func(w http.ResponseWriter) bool {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"success":true,"message":"API is disabled."}`)
		return true
	})
	path := filepath.Join(t.TempDir(), "config.json")
	opened := ""
	m, _ := newTestLoginFlow(loginApp(nil), service.LoginOptions{ConfigPath: path, URL: server.URL, Name: "lab"},
		func(url string) error { opened = url; return nil })
	startFlow(t, m)
	driveFlow(t, m, keys("token-1234abcd", keyEnter)...)
	mustContain(t, m.render(), "Error [api_disabled]:", "API is disabled.", "Docs: https://coolify.io/docs/api/ip-allowlist",
		"Open in browser  "+server.URL+"/settings/advanced")
	driveFlow(t, m, keyDown, keyDown, keyEnter)
	if opened != server.URL+"/settings/advanced" {
		t.Fatalf("opened %q", opened)
	}
	mustContain(t, m.render(), "Opened "+server.URL+"/settings/advanced", "❯ Retry")
	// Leave ends the flow as a cancellation, with nothing written.
	driveFlow(t, m, keyDown, keyDown, keyDown, keyEnter)
	if !m.finished || !errors.Is(m.err, service.ErrCancelled) {
		t.Fatalf("leave: finished=%v err=%v", m.finished, m.err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("leaving wrote the credentials file")
	}
}

type failingTransport struct{ err error }

func (f failingTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, f.err }

func TestLoginFormNamesNetworkFailuresOfTheInstanceCheck(t *testing.T) {
	tlsServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("OK")) }))
	tlsServer.Config.ErrorLog = log.New(io.Discard, "", 0) // the refused handshakes are the point
	tlsServer.StartTLS()
	defer tlsServer.Close()
	elsewhere := httptest.NewServer(http.NotFoundHandler())
	defer elsewhere.Close()
	for _, test := range []struct {
		name, url, code string
		health          func(context.Context, string) error
	}{
		{"untrusted certificate", tlsServer.URL, "tls_error", nil},
		{"unknown host", "https://coolify.invalid", "host_not_found", func(ctx context.Context, url string) error {
			client := &http.Client{Transport: failingTransport{&net.DNSError{Err: "no such host", Name: "coolify.invalid", IsNotFound: true}}}
			return coolify.CheckHealth(ctx, url, coolify.WithHTTPClient(client))
		}},
		{"not coolify", elsewhere.URL, "not_coolify", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			health := test.health
			if health == nil {
				health = func(ctx context.Context, url string) error { return coolify.CheckHealth(ctx, url) }
			}
			counted := func(ctx context.Context, url string) error { calls++; return health(ctx, url) }
			m, _ := newTestLoginFlow(loginApp(counted), service.LoginOptions{ConfigPath: filepath.Join(t.TempDir(), "c.json")}, nil)
			startFlow(t, m)
			driveFlow(t, m, keys(keyEnter, test.url, keyEnter)...)
			mustContain(t, m.render(), "✗ Check instance", "Error ["+test.code+"]:", "❯ Retry", "Go back", "Leave")
			driveFlow(t, m, keyEnter)
			if calls != 2 {
				t.Fatalf("retry made %d checks", calls)
			}
			driveFlow(t, m, keyDown, keyEnter)
			mustContain(t, m.render(), "? Coolify URL", "> "+test.url)
			driveFlow(t, m, keyEsc)
			if !errors.Is(m.err, service.ErrCancelled) {
				t.Fatalf("esc: %v", m.err)
			}
		})
	}
}

func TestLoginFormHandlesAnInstanceAlreadySaved(t *testing.T) {
	server := coolifyForLogin(t, []string{"first-token-0001", "second-token-0002"}, nil)
	path := filepath.Join(t.TempDir(), "config.json")
	if _, err := auth.Save(path, auth.Stored{Name: "home", URL: server.URL, Token: "first-token-0001"}, false); err != nil {
		t.Fatal(err)
	}
	// Saved contexts are listed under the first question, not offered.
	m, _ := newTestLoginFlow(loginApp(nil), service.LoginOptions{ConfigPath: path}, nil)
	startFlow(t, m)
	mustContain(t, m.render(), "Already saved on this machine:", "home  "+server.URL+"  (default)", "❯ Self-hosted")
	driveFlow(t, m, keys(keyEnter, server.URL, keyEnter)...)
	mustContain(t, m.render(), "? This instance is already saved", server.URL+" is saved as home.",
		"❯ Replace the token of home", "Save under a new name", "Leave")

	// A new name may not reuse a saved one, and the same token is a duplicate.
	driveFlow(t, m, keyDown, keyEnter)
	mustContain(t, m.render(), "? Context name", "> 127")
	driveFlow(t, m, keys(flowClear, "home", keyEnter)...)
	mustContain(t, m.render(), "home is already saved for "+server.URL+"; choose another name")
	driveFlow(t, m, keys(flowClear, "twin", keyEnter, "first-token-0001", keyEnter)...)
	mustContain(t, m.render(), "Error [already_saved]:", `already saved as context "home"`)

	// Going back twice reaches the choice; replacing the token skips the name.
	driveFlow(t, m, keyDown, keyEnter, flowShiftTab, flowShiftTab)
	// The choice comes back on the answer given before.
	mustContain(t, m.render(), "? This instance is already saved", "❯ Save under a new name")
	driveFlow(t, m, flowUp, keyEnter)
	mustContain(t, m.render(), "✓ Context name", "home (new token)", "? API token")
	driveFlow(t, m, keys(flowClear, "second-token-0002", keyEnter)...)
	if !m.finished || m.err != nil {
		t.Fatalf("replace: %v\n%s", m.err, m.render())
	}
	match, _ := auth.FindLogin(path, server.URL, "second-token-0002")
	if match.Duplicate != "home" || len(match.Names) != 1 {
		t.Fatalf("after replace: %+v", match)
	}
}

// TestFlowFramesNeverShrink guards the redraw: Bubble Tea clears a frame that
// shrinks from the wrong row, so every frame is padded to the tallest one,
// and the last frame stays as it was once the flow ends, for runFlow to erase.
func TestFlowFramesNeverShrink(t *testing.T) {
	actions := &cloudActions{saved: []service.SavedContext{{Name: "home", URL: "https://c.example.com", Default: true}}}
	m, _ := newTestLoginFlow(actions, service.LoginOptions{}, nil)
	startFlow(t, m)
	lines := func() int { return strings.Count(m.View().Content, "\n") + 1 }
	tallest := lines()
	driveFlow(t, m, keyEnter)
	if got := lines(); got != tallest || !strings.Contains(ansi.Strip(m.View().Content), "? Coolify URL") {
		t.Fatalf("frame after a shorter question has %d lines, want %d", got, tallest)
	}
	last := m.View().Content
	driveFlow(t, m, keyEsc)
	if !m.finished || m.View().Content != last || m.height != tallest {
		t.Fatalf("the last frame changed when the flow ended: height=%d", m.height)
	}
}

// cloudActions records the URL the form checks, without a network.
type cloudActions struct {
	checked string
	saved   []service.SavedContext
}

func (c *cloudActions) SavedContexts(string) []service.SavedContext { return c.saved }
func (c *cloudActions) CheckInstance(_ context.Context, url string) (string, error) {
	c.checked = url
	return url, nil
}
func (c *cloudActions) CheckLogin(context.Context, service.LoginOptions) (service.LoginCheck, error) {
	return service.LoginCheck{}, errors.New("unused")
}
func (c *cloudActions) SaveLogin(context.Context, service.LoginOptions, service.LoginCheck) (service.LoginResult, error) {
	return service.LoginResult{}, errors.New("unused")
}

func TestLoginFormForCoolifyCloudSkipsTheURL(t *testing.T) {
	actions := &cloudActions{}
	m, _ := newTestLoginFlow(actions, service.LoginOptions{}, nil)
	startFlow(t, m)
	driveFlow(t, m, keyDown, keyEnter)
	mustContain(t, m.render(), "✓ Instance type", "Coolify Cloud", "✓ URL", "https://app.coolify.io", "? Context name", "> cloud")
	if actions.checked != service.CloudURL {
		t.Fatalf("checked %q", actions.checked)
	}
	// Back from the name skips the URL nobody typed and lands on the choice.
	driveFlow(t, m, flowShiftTab)
	mustContain(t, m.render(), "? Where does Coolify run?", "❯ Coolify Cloud")
	driveFlow(t, m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !errors.Is(m.err, service.ErrCancelled) {
		t.Fatalf("ctrl+c: %v", m.err)
	}
}
