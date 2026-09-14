package ui

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/joaomnuno/coolship/internal/service"
)

// terminalRenderer renders as if stdout and stderr were terminals of the
// given width, at the given verbosity, with a fixed clock.
func terminalRenderer(level Verbosity, width int, now time.Time) (*Renderer, *bytes.Buffer, *bytes.Buffer) {
	var out, diagnostic bytes.Buffer
	trace := NewTrace(io.Discard)
	trace.SetLevel(level)
	r := NewRenderer(Streams{Out: &out, Err: &diagnostic, OutTerminal: true, ErrTerminal: true, Width: width, Trace: trace}, "human")
	r.now = func() time.Time { return now }
	return r, &out, &diagnostic
}

var (
	testNow    = time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	testTarget = service.TargetInfo{Application: "web", ApplicationUUID: "mm4c0zpbrzx8z96t0qiw3tff", Environment: "production", Project: "Personal", Instance: "home"}
)

func TestStatusShowsNamesOnlyOnATerminalAtNormal(t *testing.T) {
	result := service.StatusResult{Target: testTarget, Status: "running:healthy",
		LastDeployment: &service.DeploymentSummary{UUID: "nmfvbbn3bqbzor1bdhmrf7k8", Status: "finished", Commit: "0cd7c4a692347804dbd076a4d7e11c847e085473", CreatedAt: "2026-09-10T11:57:00Z"}}

	r, out, _ := terminalRenderer(VerbosityNormal, 0, testNow)
	if err := r.Status(result); err != nil {
		t.Fatal(err)
	}
	want := "Application: web\nEnvironment: production\nProject: Personal\nContext: home\nStatus: ● running:healthy\nLast deployment: nmfvbbn3 finished (0cd7c4a) 3 min ago\n"
	if out.String() != want {
		t.Errorf("normal:\n%s\nwant:\n%s", out, want)
	}

	r, out, _ = terminalRenderer(VerbosityVerbose, 0, testNow)
	if err := r.Status(result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Application: web (mm4c0zpbrzx8z96t0qiw3tff)\n") ||
		!strings.Contains(out.String(), "Last deployment: nmfvbbn3bqbzor1bdhmrf7k8 finished (0cd7c4a) "+localTime("2026-09-10T11:57:00Z")+"\n") {
		t.Errorf("verbose:\n%s", out)
	}

	// A pipe keeps the text it always had, at every verbosity.
	var piped bytes.Buffer
	if err := NewRenderer(Streams{Out: &piped}, "human").Status(result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(piped.String(), "Application: web (mm4c0zpbrzx8z96t0qiw3tff)\n") || !strings.Contains(piped.String(), "Status: running:healthy\n") ||
		!strings.Contains(piped.String(), "Last deployment: nmfvbbn3 finished (0cd7c4a) "+localTime("2026-09-10T11:57:00Z")+"\n") {
		t.Errorf("piped:\n%s", piped.String())
	}
}

func TestResultsDropUUIDsAtNormal(t *testing.T) {
	r, out, diagnostic := terminalRenderer(VerbosityNormal, 0, testNow)
	deploy := service.DeployResult{Target: testTarget, DeploymentUUID: "03dusayin5rleswixblvdqba", Status: "finished"}
	if err := r.Deploy(deploy); err != nil {
		t.Fatal(err)
	}
	if err := r.Stop(service.StopResult{Target: testTarget, Status: "exited:unhealthy"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Cancel(service.CancelResult{Target: testTarget, DeploymentUUID: "03dusayin5rleswixblvdqba", Status: "cancelled-by-user"}); err != nil {
		t.Fatal(err)
	}
	if err := r.DeploymentEvent(service.Event{Type: "deployment", DeploymentUUID: "03dusayin5rleswixblvdqba", Status: "queued"}); err != nil {
		t.Fatal(err)
	}
	if err := r.DeploymentEvent(service.Event{Type: "application", Status: "exited:unhealthy"}); err != nil {
		t.Fatal(err)
	}
	want := "Deployment: 03dusayi\nApplication: web\nStatus: finished\n" +
		"Application: web\nStatus: ● exited:unhealthy\n" +
		"Deployment: 03dusayi\nApplication: web\nStatus: cancelled-by-user\n"
	if out.String() != want {
		t.Errorf("out:\n%s\nwant:\n%s", out, want)
	}
	if diagnostic.String() != "Deployment status: queued\nApplication status: ● exited:unhealthy\n" {
		t.Errorf("diagnostic:\n%s", diagnostic)
	}

	// JSON keeps the UUID on its progress lines even on a terminal.
	r, _, diagnostic = terminalRenderer(VerbosityNormal, 0, testNow)
	r.format = "json"
	if err := r.DeploymentEvent(service.Event{Type: "deployment", DeploymentUUID: "03dusayin5rleswixblvdqba", Status: "queued"}); err != nil {
		t.Fatal(err)
	}
	if diagnostic.String() != "Deployment 03dusayin5rleswixblvdqba: queued\n" {
		t.Errorf("json progress: %q", diagnostic)
	}

	r, out, diagnostic = terminalRenderer(VerbosityDebug, 0, testNow)
	if err := r.Deploy(deploy); err != nil {
		t.Fatal(err)
	}
	if err := r.DeploymentEvent(service.Event{Type: "deployment", DeploymentUUID: "03dusayin5rleswixblvdqba", Status: "queued"}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "Deployment: 03dusayin5rleswixblvdqba\nApplication: web (mm4c0zpbrzx8z96t0qiw3tff)\nStatus: finished\n" ||
		diagnostic.String() != "Deployment 03dusayin5rleswixblvdqba: queued\n" {
		t.Errorf("debug:\n%s\n%s", out, diagnostic)
	}
}

func TestDoctorApplicationDetailOnATerminal(t *testing.T) {
	result := service.DoctorResult{Checks: []service.Check{{Name: "Application", Status: "ok", Detail: "web (mm4c0zpbrzx8z96t0qiw3tff) is running:healthy",
		Application: &service.CheckApplication{Name: "web", UUID: "mm4c0zpbrzx8z96t0qiw3tff", Status: "running:healthy"}}}}
	r, out, _ := terminalRenderer(VerbosityNormal, 0, testNow)
	if err := r.Doctor(result); err != nil {
		t.Fatal(err)
	}
	if out.String() != "[ok]   Application: web is ● running:healthy\n" {
		t.Errorf("terminal: %q", out)
	}
	var piped bytes.Buffer
	if err := NewRenderer(Streams{Out: &piped}, "human").Doctor(result); err != nil {
		t.Fatal(err)
	}
	if piped.String() != "[ok]   Application: web (mm4c0zpbrzx8z96t0qiw3tff) is running:healthy\n" {
		t.Errorf("piped: %q", piped.String())
	}
}

func TestDeploymentsTableOnATerminal(t *testing.T) {
	result := service.DeploymentsResult{Target: testTarget, Total: 2, Deployments: []service.DeploymentSummary{
		{UUID: "jky1r9crabcdefghijklmnop", Status: "finished", Commit: "0cd7c4a692347804dbd076a4d7e11c847e085473", Kind: "deploy", CreatedAt: "2026-09-10T11:56:59Z", FinishedAt: "2026-09-10T11:58:04Z"},
		{UUID: "zvvmfq5qabcdefghijklmnop", Status: "cancelled-by-user", Commit: "HEAD", Kind: "preview", PullRequest: 42, CreatedAt: "2026-09-09T08:00:00Z", FinishedAt: "2026-09-09T08:00:02Z"},
	}}
	r, out, _ := terminalRenderer(VerbosityNormal, 0, testNow)
	if err := r.Deployments(result); err != nil {
		t.Fatal(err)
	}
	want := "Deployments of web (2 of 2)\n" +
		"ID        STATUS             COMMIT   TYPE         CREATED    DURATION\n" +
		"jky1r9cr  finished           0cd7c4a  deploy       3 min ago  1:05\n" +
		"zvvmfq5q  cancelled-by-user  HEAD     preview #42  1 day ago  0:02\n"
	if out.String() != want {
		t.Errorf("wide:\n%s\nwant:\n%s", out, want)
	}

	// At 50 columns the type and the commit go; at 40 the duration too.
	r, out, _ = terminalRenderer(VerbosityNormal, 50, testNow)
	if err := r.Deployments(result); err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(out.String(), "\n"); lines[1] != "ID        STATUS             CREATED    DURATION" || lines[2] != "jky1r9cr  finished           3 min ago  1:05" {
		t.Errorf("narrow:\n%s", out)
	}
	r, out, _ = terminalRenderer(VerbosityNormal, 40, testNow)
	if err := r.Deployments(result); err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(out.String(), "\n"); lines[1] != "ID        STATUS             CREATED" {
		t.Errorf("narrower:\n%s", out)
	}

	r, out, _ = terminalRenderer(VerbosityVerbose, 0, testNow)
	if err := r.Deployments(result); err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(out.String(), "\n"); !strings.HasPrefix(lines[1], "UUID                      STATUS") || !strings.HasPrefix(lines[2], "jky1r9crabcdefghijklmnop  finished") ||
		!strings.Contains(lines[2], localTime("2026-09-10T11:56:59Z")) {
		t.Errorf("verbose:\n%s", out)
	}

	// A pipe keeps every column, the exact times, and Go's durations.
	var piped bytes.Buffer
	renderer := NewRenderer(Streams{Out: &piped, Width: 30}, "human")
	if err := renderer.Deployments(result); err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(piped.String(), "\n"); !strings.HasPrefix(lines[1], "UUID      STATUS             COMMIT   TYPE         CREATED") || !strings.HasSuffix(lines[2], "1m5s") {
		t.Errorf("piped:\n%s", piped.String())
	}
}

func TestRelativeTime(t *testing.T) {
	for value, want := range map[string]string{
		"2026-09-10T11:59:30Z": "just now",
		"2026-09-10T12:00:20Z": "just now",
		"2026-09-10T11:57:00Z": "3 min ago",
		"2026-09-10T09:00:00Z": "3 h ago",
		"2026-09-09T10:00:00Z": "1 day ago",
		"2026-09-05T12:00:00Z": "5 days ago",
		"garbage":              "garbage",
	} {
		if got := relativeTime(value, testNow); got != want {
			t.Errorf("relativeTime(%s) = %q, want %q", value, got, want)
		}
	}
	if got := relativeTime("2026-07-01T12:00:00Z", testNow); got != time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC).Local().Format("2006-01-02") {
		t.Errorf("old = %q", got)
	}
}

func TestApplicationStatusLooks(t *testing.T) {
	for status, want := range map[string]look{
		"running:healthy":      green,
		"running:unknown":      green,
		"running:unhealthy":    red,
		"starting":             yellow,
		"restarting:unknown":   yellow,
		"exited:unhealthy":     red,
		"degraded:unhealthy":   red,
		"something:unexpected": plain,
	} {
		if got := applicationStatus(status); got != want {
			t.Errorf("applicationStatus(%s) = %d, want %d", status, got, want)
		}
	}
	if got := applicationState(newPalette(true), true, "running:healthy"); got != "\x1b[32m● running:healthy\x1b[m" {
		t.Errorf("coloured = %q", got)
	}
	if got := applicationState(newPalette(true), false, "running:healthy"); got != "running:healthy" {
		t.Errorf("piped = %q", got)
	}
}
