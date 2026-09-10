package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/joaomnuno/coolship/internal/models"
)

// Deploy submits one deployment and optionally observes that exact deployment.
func (a *App) Deploy(ctx context.Context, options DeployOptions, emit Emitter) (DeployResult, error) {
	timeout, err := deployTimeout(options.Timeout)
	if err != nil {
		return DeployResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	s, err := a.prepare(ctx, options.Options)
	if err != nil {
		return DeployResult{}, err
	}
	return a.deploy(ctx, s, options, emit)
}

// alreadyQueued recognizes the server's refusal of a commit it already holds
// a queued or running deployment for, loosely, since only the words are known.
func alreadyQueued(message string) bool {
	return strings.Contains(strings.ToLower(message), "already queued")
}

// deployTimeout bounds one submission and its observation.
func deployTimeout(requested time.Duration) (time.Duration, error) {
	if requested < 0 {
		return 0, input(errors.New("deployment timeout must be positive"))
	}
	if requested == 0 {
		return 10 * time.Minute, nil
	}
	return requested, nil
}

// deploy submits and observes through an already prepared session, so a
// workflow that has just verified a binding does not resolve it again.
func (a *App) deploy(ctx context.Context, s session, options DeployOptions, emit Emitter) (DeployResult, error) {
	if options.PullRequest < 0 {
		return DeployResult{}, input(errors.New("pull request number must be positive"))
	}
	result := DeployResult{Target: targetInfo(s.project), Warnings: s.warnings, PullRequest: options.PullRequest}
	for _, warning := range s.warnings {
		if err := emitEvent(emit, Event{Type: "warning", Message: warning}); err != nil {
			return result, err
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	receipts, err := s.backend.Deploy(ctx, models.DeployRequest{ApplicationUUID: s.project.Application.UUID, Force: options.Force, PullRequest: options.PullRequest})
	if err != nil {
		var status interface{ HTTPStatusCode() int }
		if errors.As(err, &status) && status.HTTPStatusCode() == http.StatusTooManyRequests {
			return result, fmt.Errorf("server deployment queue is full; wait for a running deployment to finish, then retry: %w", err)
		}
		return result, err
	}
	var messages []string
	for _, receipt := range receipts {
		if receipt.ResourceUUID != s.project.Application.UUID {
			continue
		}
		// Coolify 4.3.18 answers a commit that is already queued with that
		// message and a fresh UUID it never queued (DeployController keeps
		// the id it generated), so the message decides, not the UUID.
		if receipt.DeploymentUUID == "" || alreadyQueued(receipt.Message) {
			if receipt.Message != "" {
				messages = append(messages, receipt.Message)
			}
			continue
		}
		if result.DeploymentUUID != "" {
			return result, fmt.Errorf("server returned multiple deployments for application %s; inspect Coolify before retrying", s.project.Application.UUID)
		}
		result.DeploymentUUID = receipt.DeploymentUUID
	}
	if result.DeploymentUUID == "" {
		// The server explains a refusal in the receipt, e.g. an unknown pull request.
		detail := "inspect Coolify before retrying"
		if len(messages) > 0 {
			detail = strings.Join(messages, "; ")
			// The server applies the queued check per pull request as well,
			// so it is decided first: preview has --force too, and only
			// another refusal is about the pull request itself.
			switch {
			case alreadyQueued(detail):
				detail += " (a deployment of this commit is already queued or running; wait for it, or pass --force to queue another)"
			case options.PullRequest > 0:
				detail += " (Coolify must already know the pull request: enable preview deployments and add it through its webhook or the UI)"
			}
		}
		return result, fmt.Errorf("server did not confirm a deployment for application %s: %s", s.project.Application.UUID, detail)
	}
	result.Status = "queued"
	timeout, _ := deployTimeout(options.Timeout)
	// The deadline is this command's --timeout; naming the flag says what to
	// change. The context decides, not the poll's error: a single request
	// that times out also satisfies errors.Is(context.DeadlineExceeded)
	// (net/http's client timeout does), and that deadline is the transport's
	// while the flag has not elapsed, so it keeps its "request timed out"
	// wording. A deployment the server no longer holds was dropped, not lost:
	// Coolify 4.3.18 answers a duplicate submission with a UUID and then
	// discards it. Any other stop keeps its own cause.
	stopped := func(err error) error {
		var status interface{ HTTPStatusCode() int }
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			return &TimeoutError{Timeout: timeout, Err: err}
		case errors.As(err, &status) && status.HTTPStatusCode() == http.StatusNotFound:
			return restate(err, "the server no longer holds this deployment (HTTP 404); Coolify drops a queued deployment that duplicates one already queued or running for the same commit; check Coolify, or pass --force to queue a rebuild")
		}
		return fmt.Errorf("observation stopped; remote deployment may still be running: %w", err)
	}
	fail := func(err error) (DeployResult, error) {
		return result, &DeploymentError{DeploymentUUID: result.DeploymentUUID, Err: err}
	}
	if err := emitEvent(emit, Event{Type: "deployment", DeploymentUUID: result.DeploymentUUID, Status: result.Status}); err != nil {
		return fail(err)
	}
	if options.NoWait {
		return result, nil
	}
	lastStatus := result.Status
	logs := buildLogCursor{}
	for {
		if err := ctx.Err(); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return fail(stopped(err))
			}
			return fail(err)
		}
		deployment, err := s.backend.GetDeployment(ctx, result.DeploymentUUID)
		if err != nil {
			return fail(stopped(err))
		}
		if deployment.UUID != result.DeploymentUUID {
			return fail(errors.New("server returned a different deployment identity; observation stopped"))
		}
		// Build output precedes the status it led to, so a terminal status line is last.
		if event, ok := logs.advance(deployment, result.DeploymentUUID); ok {
			if err := emitEvent(emit, event); err != nil {
				return fail(err)
			}
		}
		result.Status = deployment.Status
		if result.Status != lastStatus {
			if err := emitEvent(emit, Event{Type: "deployment", DeploymentUUID: result.DeploymentUUID, Status: result.Status}); err != nil {
				return fail(err)
			}
			lastStatus = result.Status
		}
		switch result.Status {
		case "finished":
			return result, nil
		case "failed", "cancelled-by-user":
			return fail(fmt.Errorf("ended with status %s", result.Status))
		}
		if err := wait(ctx, a.deps.PollInterval); err != nil {
			return fail(stopped(err))
		}
	}
}

// buildLogCursor emits each visible build log line once. Build logs are
// progress, so an unreadable document produces one warning and observation
// continues without them; a withheld document produces nothing.
type buildLogCursor struct {
	shown  int
	broken bool
}

func (c *buildLogCursor) advance(deployment models.Deployment, uuid string) (Event, bool) {
	if c.broken || deployment.Logs == nil {
		return Event{}, false
	}
	entries, err := models.ParseDeploymentLogs(*deployment.Logs)
	if err != nil {
		c.broken = true
		return Event{Type: "warning", Message: "Build logs are unreadable; continuing without them: " + err.Error()}, true
	}
	if c.shown > len(entries) {
		c.shown = 0
	}
	var lines []string
	for _, entry := range entries[c.shown:] {
		if entry.Hidden {
			continue
		}
		if output := strings.TrimRight(entry.Output, "\r\n"); output != "" {
			lines = append(lines, output)
		}
	}
	c.shown = len(entries)
	if len(lines) == 0 {
		return Event{}, false
	}
	return Event{Type: "build", DeploymentUUID: uuid, Logs: strings.Join(lines, "\n") + "\n"}, true
}
