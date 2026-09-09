package service

import (
	"context"
	"errors"
	"fmt"
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
		return err
	}
	if err := emitEvent(emit, Event{Type: "logs", Logs: snapshot.Logs}); err != nil {
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

// snapshotDelta preserves ordered duplicate lines and matches whole timestamped lines.
func snapshotDelta(previous, current string) (string, bool) {
	if current == previous {
		return "", false
	}
	if previous == "" {
		return current, false
	}
	before := strings.SplitAfter(previous, "\n")
	after := strings.SplitAfter(current, "\n")
	if before[len(before)-1] == "" {
		before = before[:len(before)-1]
	}
	if after[len(after)-1] == "" {
		after = after[:len(after)-1]
	}
	limit := min(len(before), len(after))
	for size := limit; size > 0; size-- {
		match := true
		for i := 0; i < size; i++ {
			if before[len(before)-size+i] != after[i] {
				match = false
				break
			}
		}
		if match {
			return strings.Join(after[size:], ""), false
		}
	}
	return current, true
}
