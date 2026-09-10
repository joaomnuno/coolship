package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/joaomnuno/coolship/internal/models"
)

// Stop asks Coolify to stop the application's containers after confirmation,
// then watches the status until it reports exited or the timeout passes. The
// server queues the job, so the first polls usually still see the old status.
// Only an application that already reports exited is left alone: Coolify's own
// Stop acts on every other status, and a crash-looping application reports
// restarting or degraded, never running.
func (a *App) Stop(ctx context.Context, options StopOptions, confirm ConfirmStop, emit Emitter) (StopResult, error) {
	timeout := options.Timeout
	if timeout < 0 {
		return StopResult{}, input(errors.New("stop timeout must be positive"))
	}
	if timeout == 0 {
		timeout = 2 * time.Minute
	}
	s, err := a.prepare(ctx, options.Options)
	if err != nil {
		return StopResult{}, err
	}
	result := StopResult{Target: targetInfo(s.project), Before: s.project.Application.Status, Status: s.project.Application.Status, Warnings: s.warnings}
	// Warnings reach stderr as events, as deploy does; the result carries them for JSON.
	for _, warning := range s.warnings {
		if err := emitEvent(emit, Event{Type: "warning", Message: warning}); err != nil {
			return result, err
		}
	}
	if isStopped(result.Before) {
		warning := fmt.Sprintf("Application %s is already stopped (%s); nothing was sent.", s.project.Application.Name, result.Before)
		result.Warnings = append(result.Warnings, warning)
		return result, emitEvent(emit, Event{Type: "warning", Message: warning})
	}
	if !options.Yes {
		if confirm == nil {
			return result, input(errors.New("stopping the application requires --yes when input is noninteractive"))
		}
		accepted, err := confirm(ctx, StopPlan{Target: result.Target, Status: result.Before})
		if err != nil {
			return result, err
		}
		if !accepted {
			return result, ErrCancelled
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	message, err := s.backend.StopApplication(ctx, s.project.Application.UUID)
	if err != nil {
		return result, fmt.Errorf("stop application: %w", err)
	}
	result.Message = message
	if err := emitEvent(emit, Event{Type: "application", Message: message, Status: result.Status}); err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		if err := wait(ctx, a.deps.PollInterval); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return result, fmt.Errorf("stop was requested, but the application still reports %s after %s; check it in Coolify", result.Status, timeout)
			}
			return result, err
		}
		application, err := s.backend.GetApplication(ctx, s.project.Application.UUID)
		if err != nil {
			return result, fmt.Errorf("stop was requested, but the application could not be read back: %w", err)
		}
		if application.Status != result.Status {
			result.Status = application.Status
			if err := emitEvent(emit, Event{Type: "application", Status: result.Status}); err != nil {
				return result, err
			}
		}
		if isStopped(result.Status) {
			return result, nil
		}
	}
}

// isStopped reads Coolify's compound status, such as exited:unhealthy, by its
// first word. The word is the container state — running, exited, restarting,
// created, paused — or degraded after a recent crash loop and starting for a
// swarm replica; every one but exited still has a container to stop, which is
// the rule Coolify's own Stop button follows.
func isStopped(status string) bool {
	return strings.HasPrefix(status, "exited")
}

// Start queues a deployment through the start action and observes it like
// deploy. Coolify has no other way to bring a stopped application back, so
// this is a deployment of the configured source, not a container start.
func (a *App) Start(ctx context.Context, options StartOptions, emit Emitter) (DeployResult, error) {
	return a.action(ctx, options, "start", nil, emit)
}

// Restart queues a restart-only deployment after confirmation. For Dockerfile
// and Docker image applications Coolify deploys in full; for the others it
// reuses the image built for the commit when it still exists.
func (a *App) Restart(ctx context.Context, options StartOptions, confirm ConfirmRestart, emit Emitter) (DeployResult, error) {
	return a.action(ctx, options, "restart", confirm, emit)
}

func (a *App) action(ctx context.Context, options StartOptions, action string, confirm ConfirmRestart, emit Emitter) (DeployResult, error) {
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
	result := DeployResult{Target: targetInfo(s.project), Action: action, Warnings: s.warnings}
	// Warnings precede the question, as with stop, so they inform the answer.
	for _, warning := range s.warnings {
		if err := emitEvent(emit, Event{Type: "warning", Message: warning}); err != nil {
			return result, err
		}
	}
	if action == "restart" && !options.Yes {
		if confirm == nil {
			return result, input(errors.New("restarting the application requires --yes when input is noninteractive"))
		}
		accepted, err := confirm(ctx, RestartPlan{Target: result.Target, Status: s.project.Application.Status})
		if err != nil {
			return result, err
		}
		if !accepted {
			return result, ErrCancelled
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var receipt models.ActionReceipt
	if action == "restart" {
		receipt, err = s.backend.RestartApplication(ctx, s.project.Application.UUID)
	} else {
		receipt, err = s.backend.StartApplication(ctx, s.project.Application.UUID, options.Force)
	}
	if err != nil {
		return result, err
	}
	if receipt.DeploymentUUID == "" {
		// The server declines with a message, e.g. a deployment already queued for this commit.
		detail := "inspect Coolify before retrying"
		if receipt.Message != "" {
			detail = receipt.Message
		}
		return result, fmt.Errorf("server did not confirm a deployment for application %s: %s", s.project.Application.UUID, detail)
	}
	result.DeploymentUUID = receipt.DeploymentUUID
	return a.observeDeployment(ctx, s, result, timeout, options.NoWait, emit)
}
