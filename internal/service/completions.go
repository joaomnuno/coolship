package service

import (
	"fmt"

	"github.com/joaomnuno/coolship/internal/project"
)

// CompletionTargets lists the named targets of the configuration this
// invocation would discover, so a shell can offer them for a target argument.
//
// It reads one file, never the network and never a token, and it answers with
// what it could read: a shell asking for completions has no way to show an
// error and no business failing the command the developer is still typing, so
// an unreadable, missing, or single-target configuration is an empty list. The
// single [project] form has no named target to complete, which is why it
// answers nothing rather than "default".
func (a *App) CompletionTargets(options Options) []CompletionTarget {
	p, err := project.Discover(project.Paths{CWD: options.CWD, ConfigPath: options.ConfigPath}, true)
	if err != nil || !p.Exists {
		return nil
	}
	names := p.Config.TargetNames()
	if len(names) == 0 {
		return nil
	}
	targets := make([]CompletionTarget, 0, len(names))
	for _, name := range names {
		binding := p.Config.Apps[name]
		targets = append(targets, CompletionTarget{
			Name:    name,
			Purpose: fmt.Sprintf("%s / %s / %s", binding.Project, binding.Environment, binding.Application),
		})
	}
	return targets
}
