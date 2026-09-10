package service

import (
	"context"
	"errors"
	"fmt"
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
		return result, err
	}
	var messages []string
	for _, receipt := range receipts {
		if receipt.ResourceUUID != s.project.Application.UUID {
			continue
		}
		if receipt.DeploymentUUID == "" {
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
			if options.PullRequest > 0 {
				detail += " (Coolify must already know the pull request: enable preview deployments and add it through its webhook or the UI)"
			}
		}
		return result, fmt.Errorf("server did not confirm a deployment for application %s: %s", s.project.Application.UUID, detail)
	}
	result.Status = "queued"
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
			return fail(err)
		}
		deployment, err := s.backend.GetDeployment(ctx, result.DeploymentUUID)
		if err != nil {
			return fail(fmt.Errorf("observation stopped; remote deployment may still be running: %w", err))
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
			return fail(fmt.Errorf("observation stopped; remote deployment may still be running: %w", err))
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
