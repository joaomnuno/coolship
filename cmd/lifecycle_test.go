package cmd_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/joaomnuno/coolship/cmd"
	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
)

// executeInteractive runs a command with an answering stdin, so prompts are
// written and read; the answers and the prompt text must stay on stderr.
func executeInteractive(t *testing.T, app cmd.Application, in string, args ...string) (string, string, error) {
	t.Helper()
	var out, diagnostic bytes.Buffer
	root := cmd.NewRootCommand(app, ui.Streams{In: strings.NewReader(in), Out: &out, Err: &diagnostic, Interactive: true}, "test-version")
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return out.String(), diagnostic.String(), err
}

func TestStopFlagsPromptAndProgress(t *testing.T) {
	target := service.TargetInfo{Application: "web", ApplicationUUID: "app-1", Environment: "production", Project: "Personal", Instance: "home"}
	app := fakeApplication{stop: func(ctx context.Context, options service.StopOptions, confirm service.ConfirmStop, emit service.Emitter) (service.StopResult, error) {
		if !options.Yes {
			accepted, err := confirm(ctx, service.StopPlan{Target: target, Status: "running:healthy"})
			if err != nil {
				return service.StopResult{}, err
			}
			if !accepted {
				return service.StopResult{}, service.ErrCancelled
			}
		}
		if options.Timeout != 45*time.Second && options.Timeout != 2*time.Minute {
			t.Fatalf("timeout %v", options.Timeout)
		}
		for _, event := range []service.Event{
			{Type: "application", Status: "running:healthy", Message: "Application stopping request queued."},
			{Type: "application", Status: "exited:unhealthy"},
		} {
			if err := emit(event); err != nil {
				return service.StopResult{}, err
			}
		}
		return service.StopResult{Target: target, Before: "running:healthy", Status: "exited:unhealthy", Message: "Application stopping request queued."}, nil
	}}
	// --yes with a timeout: no prompt, progress on stderr, result on stdout.
	out, diagnostic, err := execute(t, app, "stop", "--yes", "--timeout", "45s")
	if err != nil || out != "Application: web (app-1)\nStatus: exited:unhealthy\n" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if diagnostic != "Application stopping request queued.\nApplication status: exited:unhealthy\n" {
		t.Fatalf("stderr=%q", diagnostic)
	}
	// JSON keeps stdout to the one object; progress still goes to stderr.
	out, diagnostic, err = execute(t, app, "stop", "-y", "--format", "json")
	var result service.StopResult
	if err != nil || json.Unmarshal([]byte(out), &result) != nil || result.Status != "exited:unhealthy" || result.Before != "running:healthy" || !strings.Contains(diagnostic, "Application status") {
		t.Fatalf("json out=%q stderr=%q err=%v", out, diagnostic, err)
	}
	// Interactively the question names the application and environment, and a
	// refusal cancels without output.
	out, diagnostic, err = executeInteractive(t, app, "n\n", "stop")
	if !errors.Is(err, service.ErrCancelled) || out != "" || !strings.Contains(diagnostic, "Stop web in production?") || !strings.Contains(diagnostic, "Confirm [y/N]") {
		t.Fatalf("declined: out=%q stderr=%q err=%v", out, diagnostic, err)
	}
	out, _, err = executeInteractive(t, app, "y\n", "stop")
	if err != nil || !strings.Contains(out, "exited:unhealthy") {
		t.Fatalf("accepted: out=%q err=%v", out, err)
	}
	// Noninteractive without --yes is an input error raised by the prompter.
	_, _, err = execute(t, app, "stop")
	if !errors.Is(err, service.ErrInput) || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("noninteractive: %v", err)
	}
}

func TestStartAndRestartFlags(t *testing.T) {
	app := fakeApplication{
		start: func(_ context.Context, options service.StartOptions, emit service.Emitter) (service.DeployResult, error) {
			if !options.Force || !options.NoWait || options.Timeout != 30*time.Second || options.Yes {
				t.Fatalf("start options %+v", options)
			}
			if err := emit(service.Event{Type: "deployment", DeploymentUUID: "d-1", Status: "queued"}); err != nil {
				return service.DeployResult{}, err
			}
			return service.DeployResult{Target: service.TargetInfo{Application: "web", ApplicationUUID: "app-1"}, DeploymentUUID: "d-1", Action: "start", Status: "queued"}, nil
		},
		rstart: func(ctx context.Context, options service.StartOptions, confirm service.ConfirmRestart, _ service.Emitter) (service.DeployResult, error) {
			if options.Force || options.Timeout != 10*time.Minute {
				t.Fatalf("restart options %+v", options)
			}
			if !options.Yes {
				accepted, err := confirm(ctx, service.RestartPlan{Target: service.TargetInfo{Application: "web", Environment: "staging"}, Status: "running:healthy"})
				if err != nil {
					return service.DeployResult{}, err
				}
				if !accepted {
					return service.DeployResult{}, service.ErrCancelled
				}
			}
			return service.DeployResult{Target: service.TargetInfo{Application: "web", ApplicationUUID: "app-1"}, DeploymentUUID: "d-2", Action: "restart", Status: "finished"}, nil
		},
	}
	out, diagnostic, err := execute(t, app, "start", "--force", "--no-wait", "--timeout", "30s")
	if err != nil || out != "Deployment: d-1\nApplication: web (app-1)\nStatus: queued\n" || diagnostic != "Deployment d-1: queued\n" {
		t.Fatalf("start: out=%q stderr=%q err=%v", out, diagnostic, err)
	}
	// A result without a URL prints exactly what it did before the URL line existed.
	out, _, err = execute(t, app, "start", "--force", "--no-wait", "--timeout", "30s", "--format", "json")
	if err != nil || !strings.Contains(out, `"action":"start"`) {
		t.Fatalf("start json: out=%q err=%v", out, err)
	}
	out, _, err = execute(t, app, "restart", "--yes", "--format", "json")
	if err != nil || !strings.Contains(out, `"action":"restart"`) || !strings.Contains(out, `"status":"finished"`) {
		t.Fatalf("restart json: out=%q err=%v", out, err)
	}
	_, diagnostic, err = executeInteractive(t, app, "n\n", "restart")
	if !errors.Is(err, service.ErrCancelled) || !strings.Contains(diagnostic, "Restart web in staging?") {
		t.Fatalf("declined restart: stderr=%q err=%v", diagnostic, err)
	}
	if _, _, err := execute(t, app, "restart"); !errors.Is(err, service.ErrInput) {
		t.Fatalf("noninteractive restart without --yes: %v", err)
	}
}

func TestDeploymentsTableAndJSON(t *testing.T) {
	deployments := []service.DeploymentSummary{
		{UUID: "nmfvbbn3bqbzor1bdhmrf7k8", Status: "in_progress", Commit: "HEAD", Kind: "restart", Source: "api", CreatedAt: "2026-09-10T11:37:12.000000Z"},
		{UUID: "g5fmcvtu2wynvenbrpkhokbv", Status: "finished", Commit: "0cd7c4a692347804dbd076a4d7e11c847e085473", Kind: "preview", Source: "webhook", PullRequest: 42, CreatedAt: "2026-09-10T08:03:18.000000Z", FinishedAt: "2026-09-10T08:03:42.000000Z"},
		{UUID: "swrjkaxppr0lgzq6fi3igeov", Status: "cancelled-by-user", Commit: "0cd7c4a692347804dbd076a4d7e11c847e085473", Kind: "deploy", Source: "manual", CreatedAt: "2026-09-10T08:01:49.000000Z", FinishedAt: "2026-09-10T08:02:13.000000Z"},
		// Cancelled before the deployment job ran: Coolify never sets finished_at.
		{UUID: "df4wrh4kzrdjl5ua55dzl0yd", Status: "cancelled-by-user", Commit: "HEAD", Kind: "deploy", Source: "api", CreatedAt: "2026-09-10T12:26:17.000000Z"},
	}
	app := fakeApplication{list: func(_ context.Context, options service.DeploymentsOptions) (service.DeploymentsResult, error) {
		if options.Limit != 4 {
			t.Fatalf("limit %d", options.Limit)
		}
		return service.DeploymentsResult{Target: service.TargetInfo{Application: "web"}, Total: 14, Deployments: deployments}, nil
	}}
	out, diagnostic, err := execute(t, app, "deployments", "-n", "4")
	if err != nil || diagnostic != "" {
		t.Fatalf("stderr=%q err=%v", diagnostic, err)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 6 || lines[0] != "Deployments of web (4 of 14)" || !strings.HasPrefix(lines[1], "UUID      STATUS             COMMIT   TYPE         CREATED") {
		t.Fatalf("table:\n%s", out)
	}
	if !strings.HasPrefix(lines[2], "nmfvbbn3  in_progress        HEAD     restart      ") || !strings.HasPrefix(lines[3], "g5fmcvtu  finished           0cd7c4a  preview #42  ") ||
		!strings.HasPrefix(lines[4], "swrjkaxp  cancelled-by-user  0cd7c4a  deploy       ") || !strings.HasSuffix(lines[3], "  24s") || !strings.HasSuffix(lines[4], "  24s") {
		t.Fatalf("rows:\n%s", out)
	}
	// A terminal row without an end time has no duration; the line ends at the trimmed created time.
	if !strings.HasPrefix(lines[5], "df4wrh4k  cancelled-by-user  HEAD     deploy       ") || strings.HasSuffix(lines[5], "s") || strings.HasSuffix(lines[5], " ") {
		t.Fatalf("row without finished_at:\n%s", out)
	}
	out, _, err = execute(t, app, "deployments", "--limit", "4", "--format", "json")
	var result service.DeploymentsResult
	if err != nil || json.Unmarshal([]byte(out), &result) != nil || result.Total != 14 || len(result.Deployments) != 4 || result.Deployments[1].PullRequest != 42 || result.Deployments[3].FinishedAt != "" {
		t.Fatalf("json out=%q err=%v", out, err)
	}
	app.list = func(context.Context, service.DeploymentsOptions) (service.DeploymentsResult, error) {
		return service.DeploymentsResult{Target: service.TargetInfo{Application: "web"}}, nil
	}
	if out, _, err := execute(t, app, "deployments"); err != nil || out != "web has no deployments\n" {
		t.Fatalf("empty: out=%q err=%v", out, err)
	}
}

func TestCancelArgumentsAndPrompt(t *testing.T) {
	var seen service.CancelOptions
	app := fakeApplication{cancel: func(ctx context.Context, options service.CancelOptions, confirm service.ConfirmCancel) (service.CancelResult, error) {
		seen = options
		if !options.Yes {
			plan := service.CancelPlan{Target: service.TargetInfo{Application: "web", ApplicationUUID: "app-1", Environment: "production"},
				Deployment: service.DeploymentSummary{UUID: "d-1", Status: "in_progress", Commit: "0cd7c4a692347804dbd076a4d7e11c847e085473", PullRequest: 3}}
			accepted, err := confirm(ctx, plan)
			if err != nil {
				return service.CancelResult{}, err
			}
			if !accepted {
				return service.CancelResult{}, service.ErrCancelled
			}
		}
		uuid := options.DeploymentUUID
		if uuid == "" {
			uuid = "d-1"
		}
		return service.CancelResult{Target: service.TargetInfo{Application: "web", ApplicationUUID: "app-1"}, DeploymentUUID: uuid, Status: "cancelled-by-user", Message: "Deployment cancelled successfully."}, nil
	}}
	out, _, err := execute(t, app, "cancel", "--yes")
	if err != nil || seen.DeploymentUUID != "" || out != "Deployment: d-1\nApplication: web (app-1)\nStatus: cancelled-by-user\n" {
		t.Fatalf("implicit: out=%q seen=%+v err=%v", out, seen, err)
	}
	if _, _, err := execute(t, app, "cancel", "d-9", "-y"); err != nil || seen.DeploymentUUID != "d-9" || seen.Target != "" {
		t.Fatalf("uuid only: seen=%+v err=%v", seen, err)
	}
	if _, _, err := execute(t, app, "cancel", "api", "d-9", "-y"); err != nil || seen.DeploymentUUID != "d-9" || seen.Target != "api" {
		t.Fatalf("target and uuid: seen=%+v err=%v", seen, err)
	}
	if _, _, err := execute(t, app, "cancel", "--target", "api", "api", "d-9", "-y"); err != nil || seen.Target != "api" {
		t.Fatalf("agreeing target: seen=%+v err=%v", seen, err)
	}
	out, diagnostic, err := executeInteractive(t, app, "n\n", "cancel")
	if !errors.Is(err, service.ErrCancelled) || out != "" || !strings.Contains(diagnostic, "Cancel deployment d-1 of web?") || !strings.Contains(diagnostic, "in_progress, commit 0cd7c4a, pull request #3") {
		t.Fatalf("declined: out=%q stderr=%q err=%v", out, diagnostic, err)
	}
	out, _, err = executeInteractive(t, app, "yes\n", "cancel", "--format", "json")
	var result service.CancelResult
	if err != nil || json.Unmarshal([]byte(out), &result) != nil || result.Status != "cancelled-by-user" {
		t.Fatalf("accepted json: out=%q err=%v", out, err)
	}
	if _, _, err := execute(t, app, "cancel"); !errors.Is(err, service.ErrInput) || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("noninteractive: %v", err)
	}
}

func TestStatusShowsTheLastDeployment(t *testing.T) {
	app := fakeApplication{status: func(context.Context, service.Options) (service.StatusResult, error) {
		return service.StatusResult{Target: service.TargetInfo{Application: "web", ApplicationUUID: "app-1"}, Status: "running:healthy", URL: "https://web.example",
			LastDeployment: &service.DeploymentSummary{UUID: "nmfvbbn3bqbzor1bdhmrf7k8", Status: "finished", Commit: "0cd7c4a692347804dbd076a4d7e11c847e085473", Kind: "deploy", Source: "api", CreatedAt: "2026-09-10T11:37:12.000000Z"}}, nil
	}}
	out, _, err := execute(t, app, "status")
	if err != nil || !strings.Contains(out, "URL: https://web.example\nLast deployment: nmfvbbn3 finished (0cd7c4a) ") {
		t.Fatalf("out=%q err=%v", out, err)
	}
	out, _, err = execute(t, app, "status", "--format", "json")
	if err != nil || !strings.Contains(out, `"last_deployment":{"deployment_uuid":"nmfvbbn3bqbzor1bdhmrf7k8"`) {
		t.Fatalf("json out=%q err=%v", out, err)
	}
}

// TestDeploymentOutputEndsWithTheURL covers the last line of deploy, start,
// restart, and preview: the application in human output when the service
// says it is live, its Coolify page otherwise, and on failure the page on
// stderr so the diagnostic has somewhere to point.
func TestDeploymentOutputEndsWithTheURL(t *testing.T) {
	target := service.TargetInfo{Application: "web", ApplicationUUID: "app-1"}
	page := "https://coolify.example.com/project/p-1/environment/e-1/application/app-1/deployment/d-1"
	live := service.DeployResult{Target: target, DeploymentUUID: "d-1", Status: "finished", URL: "https://web.example.com", URLKind: "application"}
	queued := service.DeployResult{Target: target, DeploymentUUID: "d-1", Status: "queued", URL: page, URLKind: "deployment"}
	failed := service.DeployResult{Target: target, DeploymentUUID: "d-1", Status: "failed", URL: page, URLKind: "deployment"}
	failure := &service.DeploymentError{DeploymentUUID: "d-1", Err: errors.New("ended with status failed")}
	var result service.DeployResult
	var failWith error
	app := fakeApplication{
		deploy: func(context.Context, service.DeployOptions, service.Emitter) (service.DeployResult, error) {
			return result, failWith
		},
		start: func(context.Context, service.StartOptions, service.Emitter) (service.DeployResult, error) {
			return result, failWith
		},
		rstart: func(context.Context, service.StartOptions, service.ConfirmRestart, service.Emitter) (service.DeployResult, error) {
			return result, failWith
		},
	}

	result, failWith = live, nil
	out, diagnostic, err := execute(t, app, "deploy")
	if err != nil || out != "Deployment: d-1\nApplication: web (app-1)\nStatus: finished\nhttps://web.example.com\n" || diagnostic != "" {
		t.Fatalf("deploy: out=%q stderr=%q err=%v", out, diagnostic, err)
	}
	out, _, err = execute(t, app, "deploy", "--format", "json")
	if err != nil || !strings.Contains(out, `"url":"https://web.example.com","url_kind":"application"`) {
		t.Fatalf("deploy json: out=%q err=%v", out, err)
	}
	result = queued
	for _, args := range [][]string{{"deploy", "--no-wait"}, {"start", "--no-wait"}, {"restart", "--yes", "--no-wait"}, {"preview", "--pr", "7", "--no-wait"}} {
		out, diagnostic, err := execute(t, app, args...)
		if err != nil || !strings.HasSuffix(out, "Status: queued\n"+page+"\n") || diagnostic != "" {
			t.Fatalf("%v: out=%q stderr=%q err=%v", args, out, diagnostic, err)
		}
	}

	// A failure keeps stdout empty and names the page on stderr before the
	// error the boundary prints; JSON prints the result instead, URL included.
	result, failWith = failed, failure
	for _, args := range [][]string{{"deploy"}, {"start"}, {"restart", "--yes"}, {"preview", "--pr", "7"}} {
		out, diagnostic, err := execute(t, app, args...)
		if !errors.Is(err, failure) || out != "" || diagnostic != "Deployment page: "+page+"\n" {
			t.Fatalf("%v failed: out=%q stderr=%q err=%v", args, out, diagnostic, err)
		}
		out, diagnostic, err = execute(t, app, append(args, "--format", "json")...)
		if !errors.Is(err, failure) || !strings.Contains(out, `"status":"failed","url":"`+page+`","url_kind":"deployment"`) || diagnostic != "" {
			t.Fatalf("%v failed json: out=%q stderr=%q err=%v", args, out, diagnostic, err)
		}
	}
	// A refusal before anything was queued has no page to name.
	result, failWith = service.DeployResult{Target: target}, errors.New("server did not confirm a deployment")
	out, diagnostic, err = execute(t, app, "start")
	if !errors.Is(err, failWith) || out != "" || diagnostic != "" {
		t.Fatalf("refused start: out=%q stderr=%q err=%v", out, diagnostic, err)
	}
}
