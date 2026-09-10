package service

import (
	"context"

	"github.com/joaomnuno/coolship/internal/project"
)

// Unlink removes the local binding. It needs no credentials and touches
// nothing remote.
func (a *App) Unlink(ctx context.Context, options UnlinkOptions, confirm ConfirmUnlink) (UnlinkResult, error) {
	if err := ctx.Err(); err != nil {
		return UnlinkResult{}, err
	}
	p, err := project.Discover(project.Paths{CWD: options.CWD, ConfigPath: options.ConfigPath}, false)
	if err != nil {
		return UnlinkResult{}, input(err)
	}
	plan := UnlinkPlan{Path: p.ConfigPath, Binding: p.Config.Project}
	for _, name := range p.Config.TargetNames() {
		plan.Targets = append(plan.Targets, UnlinkTarget{Name: name, Binding: p.Config.Apps[name]})
	}
	if !options.Yes {
		if confirm == nil {
			return UnlinkResult{}, input(project.ErrReplacementRequired)
		}
		accepted, err := confirm(ctx, plan)
		if err != nil {
			return UnlinkResult{}, err
		}
		if !accepted {
			return UnlinkResult{}, ErrCancelled
		}
	}
	if err := ctx.Err(); err != nil {
		return UnlinkResult{}, err
	}
	if err := project.RemoveBinding(p); err != nil {
		return UnlinkResult{}, input(err)
	}
	return UnlinkResult{Path: p.ConfigPath}, nil
}
