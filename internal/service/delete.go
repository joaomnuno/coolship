package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/joaomnuno/coolship/internal/project"
)

// Delete removes the linked application from Coolify, the inverse of what init
// created. The plan is read from the server and confirmed first, because the
// server deletes the application the moment it is asked and queues the rest;
// nothing here can undo it.
//
// The local binding is kept unless Unlink says otherwise, so a deletion never
// edits a file the developer did not ask about. A kept binding then points at
// an application that is gone, which every later command reports; the next
// steps say so.
func (a *App) Delete(ctx context.Context, options DeleteOptions, confirm ConfirmDelete, emit Emitter) (DeleteResult, error) {
	s, err := a.prepare(ctx, options.Options)
	if err != nil {
		return DeleteResult{}, err
	}
	result := DeleteResult{Target: targetInfo(s.project), KeepVolumes: options.KeepVolumes, Warnings: s.warnings}
	// Warnings reach stderr as events, as every other workflow's do; the
	// result carries them for JSON.
	for _, warning := range s.warnings {
		if err := emitEvent(emit, Event{Type: "warning", Message: warning}); err != nil {
			return result, err
		}
	}
	plan := DeletePlan{
		Target:      result.Target,
		Status:      s.project.Application.Status,
		URLs:        applicationURLs(s.project.Application),
		KeepVolumes: options.KeepVolumes,
		Unlink:      options.Unlink,
	}
	if options.Unlink {
		plan.ConfigPath = s.project.Project.ConfigPath
	}
	if !options.Yes {
		if confirm == nil {
			return result, input(errors.New("deleting the application requires --yes when input is noninteractive"))
		}
		accepted, err := confirm(ctx, plan)
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
	message, err := s.backend.DeleteApplication(ctx, s.project.Application.UUID, !options.KeepVolumes)
	if err != nil {
		return result, fmt.Errorf("delete application: %w", err)
	}
	result.Message = message
	if err := emitEvent(emit, Event{Type: "application", Message: message}); err != nil {
		return result, err
	}
	if !options.Unlink {
		return result, nil
	}
	// The application is already gone, so a binding that cannot be removed is
	// reported as a warning rather than as the command's failure.
	if err := project.RemoveBinding(s.project.Project); err != nil {
		warning := fmt.Sprintf("Application %s was deleted, but %s was left in place: %s", s.project.Application.Name, s.project.Project.ConfigPath, err)
		result.Warnings = append(result.Warnings, warning)
		return result, emitEvent(emit, Event{Type: "warning", Message: warning})
	}
	result.Unlinked = s.project.Project.ConfigPath
	return result, nil
}
