package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Dev runs a local command with the target's runtime variables injected. This
// is where resolved values belong: a shared reference such as {{team.NAME}}
// is injected as the value it resolves to, unlike env pull, which keeps the
// reference. Withheld values are reported and left to the inherited
// environment.
func (a *App) Dev(ctx context.Context, options DevOptions, emit Emitter) error {
	if a.deps.RunProcess == nil {
		return errors.New("process execution is not configured")
	}
	s, err := a.prepare(ctx, options.Options)
	if err != nil {
		return err
	}
	spec := ProcessSpec{Dir: s.project.Target.AppRoot, Args: options.Command}
	if len(spec.Args) == 0 {
		spec.Shell = strings.TrimSpace(s.project.Target.Binding.Dev)
	}
	if len(spec.Args) == 0 && spec.Shell == "" {
		return input(fmt.Errorf("no command: pass one after --, or set dev = \"...\" in %s", s.project.Project.ConfigPath))
	}
	for _, warning := range s.warnings {
		if err := emitEvent(emit, Event{Type: "warning", Message: warning}); err != nil {
			return err
		}
	}
	variables, err := s.backend.ListEnvironmentVariables(ctx, s.project.Application.UUID)
	if err != nil {
		return err
	}
	var withheld []string
	for _, variable := range variables {
		if variable.IsPreview != options.Preview || !variable.IsRuntime {
			continue
		}
		value := variable.RealValue
		if value == nil {
			value = variable.Value
		}
		if value == nil {
			withheld = append(withheld, variable.Key)
			continue
		}
		spec.Env = append(spec.Env, variable.Key+"="+*value)
	}
	sort.Strings(spec.Env)
	if len(withheld) > 0 {
		sort.Strings(withheld)
		if err := emitEvent(emit, Event{Type: "warning", Message: fmt.Sprintf("%d value(s) are withheld by Coolify and come from your own environment if at all: %s", len(withheld), strings.Join(withheld, ", "))}); err != nil {
			return err
		}
	}
	if err := emitEvent(emit, Event{Type: "dev", Message: fmt.Sprintf("Running with %d %s variable(s) of %s in %s", len(spec.Env), scopeName(options.Preview), s.project.Application.Name, spec.Dir)}); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	code, err := a.deps.RunProcess(ctx, spec)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return err
		}
		return fmt.Errorf("run command: %w", err)
	}
	if code != 0 {
		return &ExitError{Code: code}
	}
	return nil
}
