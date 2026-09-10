package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
)

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
		return logsError(err, s.project.Application.Status)
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
				return logsError(err, s.project.Application.Status)
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
