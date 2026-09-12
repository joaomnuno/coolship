package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/joaomnuno/coolship/internal/models"
)

func record(uuid, status, commit string) models.DeploymentRecord {
	return models.DeploymentRecord{UUID: uuid, Status: status, Commit: commit, CommitMessage: "cool container\n", IsAPI: true, ServerName: "Master",
		CreatedAt: "2026-09-10T11:37:12.000000Z", UpdatedAt: "2026-09-10T11:37:37.000000Z", FinishedAt: "2026-09-10T11:37:36.000000Z"}
}

func TestStopConfirmsThenWaitsForTheStatusToReportExited(t *testing.T) {
	f := newBackend()
	app, _, _ := testApp(f)
	options := StopOptions{Options: linkedOptions(t)}
	if _, err := app.Stop(context.Background(), options, nil, nil); !errors.Is(err, ErrInput) || f.calls["stop"] != 0 {
		t.Fatalf("noninteractive without --yes: err=%v stops=%d", err, f.calls["stop"])
	}
	declined := func(context.Context, StopPlan) (bool, error) { return false, nil }
	if _, err := app.Stop(context.Background(), options, declined, nil); !errors.Is(err, ErrCancelled) || f.calls["stop"] != 0 {
		t.Fatalf("declined: err=%v stops=%d", err, f.calls["stop"])
	}
	var plan StopPlan
	accepted := func(_ context.Context, p StopPlan) (bool, error) { plan = p; return true, nil }
	var events []string
	result, err := app.Stop(context.Background(), options, accepted, func(e Event) error { events = append(events, e.Type+":"+e.Status+":"+e.Message); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != "running:healthy" || plan.Target.ApplicationUUID != "app-1" || plan.Target.Environment != "production" {
		t.Fatalf("plan %+v", plan)
	}
	if result.Before != "running:healthy" || result.Status != "exited:unhealthy" || result.Message != "Application stopping request queued." || f.calls["stop"] != 1 {
		t.Fatalf("result=%+v calls=%v", result, f.calls)
	}
	// The receipt is reported once, then only status changes; the still-running poll is silent.
	if !reflect.DeepEqual(events, []string{"application:running:healthy:Application stopping request queued.", "application:exited:unhealthy:"}) {
		t.Fatalf("events %v", events)
	}
	if f.calls["projects"] != 3 {
		t.Fatalf("each invocation prepares once: %v", f.calls)
	}
	// An application that already reports exited is left alone, with a warning and no request.
	f.application.Status = "exited:unhealthy"
	f.environments[0].Applications[0] = f.application
	f.stopped = false
	warnings := 0
	result, err = app.Stop(context.Background(), StopOptions{Options: options.Options, Yes: true}, nil, func(e Event) error {
		if e.Type == "warning" {
			warnings++
		}
		return nil
	})
	if err != nil || f.calls["stop"] != 1 || warnings != 1 || len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "already stopped (exited:unhealthy)") {
		t.Fatalf("already stopped: result=%+v err=%v calls=%v warnings=%d", result, err, f.calls, warnings)
	}
}

func TestStopSendsForEveryStatusButExitedAndWaitsForExited(t *testing.T) {
	// Coolify reports a crash-looping container as restarting, a recent crash
	// loop as degraded, a swarm replica as starting, and other Docker states as
	// they are; its own Stop acts on every status that is not exited, and the
	// server's action has no precondition at all.
	for _, before := range []string{"restarting:unhealthy", "degraded:unhealthy", "starting:unhealthy", "created:unhealthy", "paused:healthy", "running:unhealthy"} {
		f := newBackend()
		f.application.Status = before
		f.environments[0].Applications[0] = f.application
		// The queued job's first effects can be other non-exited readings; only exited ends the wait.
		f.stopStatuses = []string{before, "restarting:unhealthy", "degraded:unhealthy", "exited:unhealthy"}
		app, _, _ := testApp(f)
		var plan StopPlan
		accepted := func(_ context.Context, p StopPlan) (bool, error) { plan = p; return true, nil }
		var statuses []string
		result, err := app.Stop(context.Background(), StopOptions{Options: linkedOptions(t)}, accepted, func(e Event) error {
			if e.Type == "application" && e.Message == "" {
				statuses = append(statuses, e.Status)
			}
			return nil
		})
		if err != nil || plan.Status != before || result.Before != before || result.Status != "exited:unhealthy" || f.calls["stop"] != 1 || len(result.Warnings) != 0 {
			t.Fatalf("%s: result=%+v plan=%+v err=%v calls=%v", before, result, plan, err, f.calls)
		}
		if n := len(statuses); n < 2 || statuses[n-1] != "exited:unhealthy" || statuses[n-2] != "degraded:unhealthy" {
			t.Fatalf("%s: the wait ended at %v, want to pass degraded and end at exited", before, statuses)
		}
	}
}

func TestStopReportsTimeoutAndRequestFailures(t *testing.T) {
	f := newBackend()
	f.stopStatuses = []string{"running:healthy"}
	app, _, _ := testApp(f)
	options := StopOptions{Options: linkedOptions(t), Yes: true, Timeout: 20 * time.Millisecond}
	result, err := app.Stop(context.Background(), options, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "still reports running:healthy") || result.Status != "running:healthy" {
		t.Fatalf("timeout: result=%+v err=%v", result, err)
	}
	// A crash loop that keeps reporting degraded is not mistaken for stopped.
	f.stopStatuses = []string{"degraded:unhealthy"}
	result, err = app.Stop(context.Background(), options, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "still reports degraded:unhealthy") || result.Status != "degraded:unhealthy" {
		t.Fatalf("degraded timeout: result=%+v err=%v", result, err)
	}
	f.stopError = errors.New("boom")
	if _, err := app.Stop(context.Background(), options, nil, nil); err == nil || !strings.Contains(err.Error(), "stop application: boom") {
		t.Fatalf("request failure: %v", err)
	}
	if _, err := app.Stop(context.Background(), StopOptions{Options: options.Options, Timeout: -1}, nil, nil); !errors.Is(err, ErrInput) {
		t.Fatalf("negative timeout: %v", err)
	}
}

func TestStartQueuesThroughTheActionAndObservesLikeDeploy(t *testing.T) {
	f := newBackend()
	f.deployments = []models.Deployment{{UUID: "deploy-1", Status: "queued"}, {UUID: "deploy-1", Status: "in_progress"}, {UUID: "deploy-1", Status: "finished"}}
	app, _, _ := testApp(f)
	var states []string
	result, err := app.Start(context.Background(), StartOptions{Options: linkedOptions(t), Force: true}, func(e Event) error {
		if e.Type == "deployment" {
			states = append(states, e.Status)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "start" || result.DeploymentUUID != "deploy-1" || result.Status != "finished" || !reflect.DeepEqual(states, []string{"queued", "in_progress", "finished"}) {
		t.Fatalf("result=%+v states=%v", result, states)
	}
	if result.URL != "https://app.example.com" || result.URLKind != "application" {
		t.Fatalf("url=%q kind=%q", result.URL, result.URLKind)
	}
	if f.calls["start"] != 1 || !f.lastForce || f.calls["deploy"] != 0 || f.calls["projects"] != 1 {
		t.Fatalf("calls=%v force=%t", f.calls, f.lastForce)
	}
	// --no-wait returns the queued identity without reading the deployment.
	f.calls = map[string]int{}
	result, err = app.Start(context.Background(), StartOptions{Options: linkedOptions(t), NoWait: true}, nil)
	if err != nil || result.Status != "queued" || f.calls["deployment"] != 0 || f.lastForce {
		t.Fatalf("no-wait: result=%+v err=%v calls=%v", result, err, f.calls)
	}
	if result.URL != deploymentPageOfDeploy1 || result.URLKind != "deployment" {
		t.Fatalf("no-wait url=%q kind=%q", result.URL, result.URLKind)
	}
	// A message-only answer is the server declining, e.g. a deployment already queued.
	f.receipt = models.ActionReceipt{Message: "Deployment already queued for this commit."}
	_, err = app.Start(context.Background(), StartOptions{Options: linkedOptions(t)}, nil)
	if err == nil || !strings.Contains(err.Error(), "did not confirm") || !strings.Contains(err.Error(), "already queued") {
		t.Fatalf("declined: %v", err)
	}
	// A failed deployment is reported with its identity, as deploy does.
	f.receipt = models.ActionReceipt{DeploymentUUID: "deploy-1"}
	f.deployments = []models.Deployment{{UUID: "deploy-1", Status: "failed"}}
	result, err = app.Start(context.Background(), StartOptions{Options: linkedOptions(t)}, nil)
	var deploymentError *DeploymentError
	if !errors.As(err, &deploymentError) || deploymentError.DeploymentUUID != "deploy-1" {
		t.Fatalf("failed: %v", err)
	}
	if result.URL != deploymentPageOfDeploy1 || result.URLKind != "deployment" {
		t.Fatalf("failed url=%q kind=%q", result.URL, result.URLKind)
	}
	if _, err := app.Start(context.Background(), StartOptions{Options: linkedOptions(t), Timeout: -1}, nil); !errors.Is(err, ErrInput) {
		t.Fatalf("negative timeout: %v", err)
	}
}

func TestRestartRequiresConfirmationThenObserves(t *testing.T) {
	f := newBackend()
	app, _, _ := testApp(f)
	options := StartOptions{Options: linkedOptions(t)}
	if _, err := app.Restart(context.Background(), options, nil, nil); !errors.Is(err, ErrInput) || f.calls["restart"] != 0 {
		t.Fatalf("noninteractive without --yes: err=%v calls=%v", err, f.calls)
	}
	declined := func(context.Context, RestartPlan) (bool, error) { return false, nil }
	if _, err := app.Restart(context.Background(), options, declined, nil); !errors.Is(err, ErrCancelled) || f.calls["restart"] != 0 {
		t.Fatalf("declined: err=%v", err)
	}
	var plan RestartPlan
	accepted := func(_ context.Context, p RestartPlan) (bool, error) { plan = p; return true, nil }
	result, err := app.Restart(context.Background(), options, accepted, nil)
	if err != nil || result.Action != "restart" || result.Status != "finished" || plan.Status != "running:healthy" || f.calls["restart"] != 1 || f.calls["start"] != 0 {
		t.Fatalf("result=%+v plan=%+v err=%v calls=%v", result, plan, err, f.calls)
	}
	if result.URL != "https://app.example.com" || result.URLKind != "application" {
		t.Fatalf("url=%q kind=%q", result.URL, result.URLKind)
	}
	if _, err := app.Restart(context.Background(), StartOptions{Options: options.Options, Yes: true, NoWait: true}, nil, nil); err != nil || f.calls["restart"] != 2 {
		t.Fatalf("--yes: err=%v calls=%v", err, f.calls)
	}
}

func TestDeploymentsSummarizesHistoryWithoutBuildLogs(t *testing.T) {
	f := newBackend()
	preview := record("d-preview", "finished", "abcdef0123456789")
	preview.PullRequest = 7
	preview.IsWebhook = true
	restart := record("d-restart", "in_progress", "HEAD")
	restart.RestartOnly, restart.FinishedAt = true, ""
	rollback := record("d-rollback", "failed", "abcdef0123456789")
	rollback.Rollback, rollback.IsAPI = true, false
	f.history = []models.DeploymentRecord{restart, preview, rollback, record("d-old", "cancelled-by-user", "abc")}
	app, _, _ := testApp(f)
	options := DeploymentsOptions{Options: linkedOptions(t), Limit: 3}
	result, err := app.Deployments(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 4 || len(result.Deployments) != 3 {
		t.Fatalf("result %+v", result)
	}
	want := []DeploymentSummary{
		{UUID: "d-restart", Status: "in_progress", Commit: "HEAD", CommitMessage: "cool container", Kind: "restart", Source: "api", Server: "Master", CreatedAt: "2026-09-10T11:37:12.000000Z"},
		{UUID: "d-preview", Status: "finished", Commit: "abcdef0123456789", CommitMessage: "cool container", Kind: "preview", Source: "webhook", PullRequest: 7, Server: "Master", CreatedAt: "2026-09-10T11:37:12.000000Z", FinishedAt: "2026-09-10T11:37:36.000000Z"},
		{UUID: "d-rollback", Status: "failed", Commit: "abcdef0123456789", CommitMessage: "cool container", Kind: "rollback", Source: "manual", Server: "Master", CreatedAt: "2026-09-10T11:37:12.000000Z", FinishedAt: "2026-09-10T11:37:36.000000Z"},
	}
	if !reflect.DeepEqual(result.Deployments, want) {
		t.Fatalf("deployments:\n%+v\nwant:\n%+v", result.Deployments, want)
	}
	data, _ := json.Marshal(result)
	if strings.Contains(strings.ToLower(string(data)), `"logs"`) {
		t.Fatalf("history result carries logs: %s", data)
	}
	if _, err := app.Deployments(context.Background(), DeploymentsOptions{Options: options.Options}); !errors.Is(err, ErrInput) {
		t.Fatalf("zero limit: %v", err)
	}
	f.historyError = errors.New("boom")
	if _, err := app.Deployments(context.Background(), options); err == nil || !strings.Contains(err.Error(), "read deployment history: boom") {
		t.Fatalf("history failure: %v", err)
	}
}

func TestStatusAddsTheLastDeploymentWhenReadable(t *testing.T) {
	f := newBackend()
	app, _, _ := testApp(f)
	options := linkedOptions(t)
	result, err := app.Status(context.Background(), options)
	if err != nil || result.LastDeployment != nil || len(result.Warnings) != 0 {
		t.Fatalf("empty history: result=%+v err=%v", result, err)
	}
	f.history = []models.DeploymentRecord{record("d-new", "finished", "0cd7c4a692347804dbd076a4d7e11c847e085473"), record("d-old", "failed", "abc")}
	result, err = app.Status(context.Background(), options)
	if err != nil || result.LastDeployment == nil || result.LastDeployment.UUID != "d-new" || result.LastDeployment.Status != "finished" || result.Status != "running:healthy" {
		t.Fatalf("with history: result=%+v err=%v", result, err)
	}
	f.historyError = errors.New("boom")
	result, err = app.Status(context.Background(), options)
	if err != nil || result.LastDeployment != nil || len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "boom") || result.Status != "running:healthy" {
		t.Fatalf("unreadable history must warn, not fail: result=%+v err=%v", result, err)
	}
	// A cancelled or timed-out history read is a real error, not a warning:
	// Ctrl-C during status must exit 130, not report success with a note.
	f.historyError = context.Canceled
	result, err = app.Status(context.Background(), options)
	if !errors.Is(err, context.Canceled) || result.LastDeployment != nil || len(result.Warnings) != 0 {
		t.Fatalf("cancelled history read must propagate, not warn: result=%+v err=%v", result, err)
	}
	f.historyError = context.DeadlineExceeded
	result, err = app.Status(context.Background(), options)
	if !errors.Is(err, context.DeadlineExceeded) || result.LastDeployment != nil || len(result.Warnings) != 0 {
		t.Fatalf("timed-out history read must propagate, not warn: result=%+v err=%v", result, err)
	}
}

func TestCancelRefusesNoneOrSeveralAndConfirmsOne(t *testing.T) {
	f := newBackend()
	app, _, _ := testApp(f)
	options := CancelOptions{Options: linkedOptions(t)}
	f.history = []models.DeploymentRecord{record("d-done", "finished", "abc")}
	if _, err := app.Cancel(context.Background(), options, nil); !errors.Is(err, ErrInput) || !strings.Contains(err.Error(), "no deployment of api is queued or in progress") {
		t.Fatalf("none: %v", err)
	}
	f.history = []models.DeploymentRecord{record("d-b", "queued", "HEAD"), record("d-a", "in_progress", "abc"), record("d-done", "finished", "abc")}
	_, err := app.Cancel(context.Background(), options, nil)
	if !errors.Is(err, ErrInput) || !strings.Contains(err.Error(), "2 deployments") || !strings.Contains(err.Error(), "d-b (queued)") || !strings.Contains(err.Error(), "d-a (in_progress)") {
		t.Fatalf("several: %v", err)
	}
	f.history = f.history[1:]
	if _, err := app.Cancel(context.Background(), options, nil); !errors.Is(err, ErrInput) || f.calls["cancel"] != 0 {
		t.Fatalf("noninteractive without --yes: err=%v calls=%v", err, f.calls)
	}
	declined := func(context.Context, CancelPlan) (bool, error) { return false, nil }
	if _, err := app.Cancel(context.Background(), options, declined); !errors.Is(err, ErrCancelled) || f.calls["cancel"] != 0 {
		t.Fatalf("declined: %v", err)
	}
	var plan CancelPlan
	accepted := func(_ context.Context, p CancelPlan) (bool, error) { plan = p; return true, nil }
	result, err := app.Cancel(context.Background(), options, accepted)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Deployment.UUID != "d-a" || plan.Deployment.Status != "in_progress" || result.DeploymentUUID != "d-a" || result.Status != "cancelled-by-user" ||
		!reflect.DeepEqual(f.cancelled, []string{"d-a"}) || result.Message == "" {
		t.Fatalf("plan=%+v result=%+v cancelled=%v", plan, result, f.cancelled)
	}
	// A named deployment is read, checked against the binding, and refused when it cannot be cancelled.
	for _, test := range []struct{ uuid, want string }{
		{"missing", "was not found"},
		{"theirs", "does not belong to application api"},
		{"old", "is finished; only queued or in-progress"},
		{"has space", "not a valid identifier"},
	} {
		_, err := app.Cancel(context.Background(), CancelOptions{Options: options.Options, DeploymentUUID: test.uuid, Yes: true}, nil)
		if !errors.Is(err, ErrInput) || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: err=%v, want %q", test.uuid, err, test.want)
		}
	}
	f.deployments = []models.Deployment{{UUID: "deploy-1", Status: "queued", Commit: "HEAD"}}
	result, err = app.Cancel(context.Background(), CancelOptions{Options: options.Options, DeploymentUUID: "deploy-1", Yes: true}, nil)
	// The five earlier attempts each read the history; a named deployment never does.
	if err != nil || result.DeploymentUUID != "deploy-1" || f.calls["history"] != 5 || !reflect.DeepEqual(f.cancelled, []string{"d-a", "deploy-1"}) {
		t.Fatalf("named: result=%+v err=%v calls=%v cancelled=%v", result, err, f.calls, f.cancelled)
	}
	// The server's refusal (a 400 with its explanation) is an operation failure.
	f.cancelError = errors.New("HTTP 400 Bad Request: Deployment cannot be cancelled. Current status: finished")
	if _, err := app.Cancel(context.Background(), CancelOptions{Options: options.Options, DeploymentUUID: "deploy-1", Yes: true}, nil); err == nil || errors.Is(err, ErrInput) || !strings.Contains(err.Error(), "Current status: finished") {
		t.Fatalf("refusal: %v", err)
	}
}
