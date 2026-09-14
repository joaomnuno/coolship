package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/joaomnuno/coolship/internal/models"
)

// logsNotRunningRetries bounds how many times a "not running" refusal is
// retried while the application's own status still says running. At the
// default poll interval this spans about ten seconds, enough to ride out a
// Compose recreate's container hand-off right after a deployment.
const logsNotRunningRetries = 5

// Logs reads timestamped runtime snapshots. Follow is best effort because the API has no cursor.
func (a *App) Logs(ctx context.Context, options LogsOptions, emit Emitter) error {
	if options.Lines <= 0 {
		return input(errors.New("log line count must be positive"))
	}
	s, err := a.prepare(ctx, options.Options)
	if err != nil {
		return err
	}
	for _, warning := range s.warnings {
		if err := emitEvent(emit, Event{Type: "warning", Message: warning}); err != nil {
			return err
		}
	}
	snapshot, err := s.backend.Logs(ctx, s.project.Application.UUID, options.Lines)
	if err != nil {
		snapshot, err = a.retryLogsIfRunning(ctx, s, options.Lines, err)
		if err != nil {
			return err
		}
	}
	if err := emitEvent(emit, Event{Type: "logs", Logs: joinLines(splitLines(snapshot.Logs))}); err != nil {
		return err
	}
	if !options.Follow {
		return nil
	}
	previous := snapshot.Logs
	for {
		if err := wait(ctx, a.deps.PollInterval); err != nil {
			return err
		}
		snapshot, err := s.backend.Logs(ctx, s.project.Application.UUID, options.Lines)
		if err != nil {
			if notRunning(err) {
				// The status read when the follow started is stale by now;
				// the one at the refusal says what became of the container.
				return logsError(err, currentStatus(ctx, s))
			}
			return fmt.Errorf("log follow stopped: %w", err)
		}
		added, reset := snapshotDelta(previous, snapshot.Logs)
		previous = snapshot.Logs
		if reset {
			if err := emitEvent(emit, Event{Type: "warning", Message: "Log snapshots no longer overlap; logs may have rotated or the container restarted. Gaps or duplicates are possible."}); err != nil {
				return err
			}
		}
		if added != "" {
			if err := emitEvent(emit, Event{Type: "logs", Logs: added}); err != nil {
				return err
			}
		}
	}
}

// notRunning recognizes the adapter's refusal for an application without a
// running container, through an interface so this package stays free of HTTP.
func notRunning(err error) bool {
	var refusal interface{ NotRunning() bool }
	return errors.As(err, &refusal) && refusal.NotRunning()
}

// currentStatus re-reads the application's status once time has passed since
// prepare read it. A failed read leaves the status unknown rather than
// repeating one that no longer holds.
func currentStatus(ctx context.Context, s session) string {
	application, err := s.backend.GetApplication(ctx, s.project.Application.UUID)
	if err != nil {
		return ""
	}
	return application.Status
}

// logsError names the application's observed status when the server has no
// container to read logs from; other failures pass through unchanged.
func logsError(err error, status string) error {
	if !notRunning(err) {
		return err
	}
	if status == "" {
		status = "unknown"
	}
	return restate(err, "application is not running (status %s); deploy it, or start it in Coolify, before reading its logs", status)
}

// retryLogsIfRunning re-issues a "not running" refusal while the
// application's own status still says running: the logs endpoint resolves
// the container by name and can briefly disagree with the aggregated status
// during a Compose recreate, or when a one-shot service has exited. Any
// other failure, and a status that does not say running, pass through for
// logsError to decorate as before.
func (a *App) retryLogsIfRunning(ctx context.Context, s session, lines int, first error) (models.LogSnapshot, error) {
	if !notRunning(first) {
		return models.LogSnapshot{}, first
	}
	status := currentStatus(ctx, s)
	if !strings.HasPrefix(status, "running") {
		return models.LogSnapshot{}, logsError(first, status)
	}
	err := first
	for range logsNotRunningRetries {
		if waitErr := wait(ctx, a.deps.PollInterval); waitErr != nil {
			return models.LogSnapshot{}, waitErr
		}
		var snapshot models.LogSnapshot
		snapshot, err = s.backend.Logs(ctx, s.project.Application.UUID, lines)
		if err == nil {
			return snapshot, nil
		}
		if !notRunning(err) {
			return models.LogSnapshot{}, err
		}
		status = currentStatus(ctx, s)
		if !strings.HasPrefix(status, "running") {
			return models.LogSnapshot{}, logsError(err, status)
		}
	}
	return models.LogSnapshot{}, stillNotRunningError(err, status)
}

// stillNotRunningError reports that the container never showed up within the
// retry window, even though Coolify's aggregated status says running.
func stillNotRunningError(err error, status string) error {
	if status == "" {
		status = "unknown"
	}
	return restate(err, "Coolify reports the application as %s but has no running container to read logs from yet; this happens briefly after a deployment, or when a Compose service has exited. Retry in a moment.", status)
}

// snapshotDelta preserves ordered duplicate lines and matches whole timestamped
// lines. Lines are compared without terminators: the server omits the newline
// after the final line, so the same line gains one when a later snapshot places
// it earlier. Returned text ends every line with a newline so consumers can
// concatenate chunks.
func snapshotDelta(previous, current string) (string, bool) {
	if current == previous {
		return "", false
	}
	before := splitLines(previous)
	after := splitLines(current)
	if len(before) == 0 {
		return joinLines(after), false
	}
	if len(after) == 0 {
		return "", true
	}
	for size := min(len(before), len(after)); size > 0; size-- {
		if slices.Equal(before[len(before)-size:], after[:size]) {
			return joinLines(after[size:]), false
		}
	}
	return joinLines(after), true
}

func splitLines(text string) []string {
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}
