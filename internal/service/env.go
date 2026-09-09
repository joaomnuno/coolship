package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/joaomnuno/coolship/internal/envfile"
	"github.com/joaomnuno/coolship/internal/models"
)

// EnvOptions selects one scope of one application's variables and one local
// file. Preview is a separate dimension from the remote environment: Coolify
// keeps a preview-deployment copy of every variable beside the regular one.
type EnvOptions struct {
	Options
	File    string
	Preview bool
}

type EnvPushOptions struct {
	EnvOptions
	Prune bool
	Force bool
	Yes   bool
}

// EnvChange is one key's difference between the local file and the remote
// scope. Values are carried so a renderer can reveal them on request; they are
// masked by default.
type EnvChange struct {
	Key    string `json:"key"`
	Local  string `json:"local,omitempty"`
	Remote string `json:"remote,omitempty"`
}

// EnvDiffResult classifies keys from the local file's point of view.
type EnvDiffResult struct {
	Target    TargetInfo  `json:"target"`
	Scope     string      `json:"scope"`
	File      string      `json:"file"`
	Added     []EnvChange `json:"added,omitempty"`    // local only: push creates
	Changed   []EnvChange `json:"changed,omitempty"`  // both, different: push updates
	Removed   []EnvChange `json:"removed,omitempty"`  // remote only: push --prune deletes
	Withheld  []string    `json:"withheld,omitempty"` // remote value unavailable
	Unchanged int         `json:"unchanged"`
	Warnings  []string    `json:"warnings,omitempty"`
}

func (r EnvDiffResult) Clean() bool {
	return len(r.Added) == 0 && len(r.Changed) == 0 && len(r.Removed) == 0
}

type EnvPullResult struct {
	Target   TargetInfo `json:"target"`
	Scope    string     `json:"scope"`
	File     string     `json:"file"`
	Written  []string   `json:"written,omitempty"`
	Kept     []string   `json:"kept,omitempty"`     // local-only keys preserved
	Withheld []string   `json:"withheld,omitempty"` // not written; value unavailable
	Warnings []string   `json:"warnings,omitempty"`
}

type EnvPushPlan struct {
	Target  TargetInfo  `json:"target"`
	Scope   string      `json:"scope"`
	File    string      `json:"file"`
	Create  []EnvChange `json:"create,omitempty"`
	Update  []EnvChange `json:"update,omitempty"`
	Delete  []EnvChange `json:"delete,omitempty"`
	Skipped []string    `json:"skipped,omitempty"` // withheld keys not forced
}

func (p EnvPushPlan) Empty() bool { return len(p.Create)+len(p.Update)+len(p.Delete) == 0 }

type ConfirmPush func(context.Context, EnvPushPlan) (bool, error)

type EnvPushResult struct {
	Plan     EnvPushPlan `json:"plan"`
	Warnings []string    `json:"warnings,omitempty"`
}

func scopeName(preview bool) string {
	if preview {
		return "preview"
	}
	return "regular"
}

// envState is the shared read both diff-like workflows start from.
type envState struct {
	session session
	file    *envfile.File
	path    string
	remote  map[string]models.EnvironmentVariable
	scope   string
}

func (a *App) envState(ctx context.Context, options EnvOptions) (envState, error) {
	s, err := a.prepare(ctx, options.Options)
	if err != nil {
		return envState{}, err
	}
	path := options.File
	if path == "" {
		path = ".env"
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(s.project.Target.AppRoot, path)
	}
	file, err := envfile.Read(path)
	if err != nil {
		return envState{}, input(err)
	}
	variables, err := s.backend.ListEnvironmentVariables(ctx, s.project.Application.UUID)
	if err != nil {
		return envState{}, err
	}
	remote := map[string]models.EnvironmentVariable{}
	for _, variable := range variables {
		if variable.IsPreview != options.Preview {
			continue
		}
		if _, duplicate := remote[variable.Key]; duplicate {
			return envState{}, fmt.Errorf("server returned %q twice in the %s scope; inspect Coolify before syncing", variable.Key, scopeName(options.Preview))
		}
		remote[variable.Key] = variable
	}
	return envState{session: s, file: file, path: path, remote: remote, scope: scopeName(options.Preview)}, nil
}

// EnvDiff compares the local file with one remote scope. Shared references
// such as {{team.NAME}} compare by their template, never by resolved value, so
// a push cannot replace a reference with the secret it resolved to.
func (a *App) EnvDiff(ctx context.Context, options EnvOptions) (EnvDiffResult, error) {
	state, err := a.envState(ctx, options)
	if err != nil {
		return EnvDiffResult{}, err
	}
	result := EnvDiffResult{Target: targetInfo(state.session.project), Scope: state.scope, File: state.path, Warnings: state.session.warnings}
	classify(state, &result)
	return result, nil
}

func classify(state envState, result *EnvDiffResult) {
	local := map[string]string{}
	for _, entry := range state.file.Entries() {
		local[entry.Key] = entry.Value
	}
	for _, key := range sortedKeys(local) {
		remote, exists := state.remote[key]
		switch {
		case !exists:
			result.Added = append(result.Added, EnvChange{Key: key, Local: local[key]})
		case remote.Value == nil:
			result.Withheld = append(result.Withheld, key)
		case *remote.Value != local[key]:
			result.Changed = append(result.Changed, EnvChange{Key: key, Local: local[key], Remote: *remote.Value})
		default:
			result.Unchanged++
		}
	}
	for _, key := range sortedKeys(state.remote) {
		if _, exists := local[key]; exists {
			continue
		}
		remote := state.remote[key]
		if remote.Value == nil {
			result.Withheld = append(result.Withheld, key)
			continue
		}
		result.Removed = append(result.Removed, EnvChange{Key: key, Remote: *remote.Value})
	}
	sort.Strings(result.Withheld)
}

// EnvPull writes the remote scope into the local file. Keys only present
// locally are kept, withheld values are never invented, and comments and
// ordering in the file survive.
func (a *App) EnvPull(ctx context.Context, options EnvOptions) (EnvPullResult, error) {
	state, err := a.envState(ctx, options)
	if err != nil {
		return EnvPullResult{}, err
	}
	result := EnvPullResult{Target: targetInfo(state.session.project), Scope: state.scope, File: state.path, Warnings: state.session.warnings}
	local := map[string]bool{}
	for _, entry := range state.file.Entries() {
		local[entry.Key] = true
	}
	for _, key := range sortedKeys(state.remote) {
		variable := state.remote[key]
		if variable.Value == nil {
			result.Withheld = append(result.Withheld, key)
			if local[key] {
				continue // keep whatever the developer already has
			}
			state.file.Comment(key + " is withheld by Coolify (shown once); set it here yourself")
			continue
		}
		if err := state.file.Set(key, *variable.Value); err != nil {
			return result, fmt.Errorf("%s: %w", key, err)
		}
		result.Written = append(result.Written, key)
	}
	for _, entry := range state.file.Entries() {
		if _, remote := state.remote[entry.Key]; !remote {
			result.Kept = append(result.Kept, entry.Key)
		}
	}
	if len(result.Withheld) > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("%d value(s) are withheld by Coolify and were not written: %s", len(result.Withheld), strings.Join(result.Withheld, ", ")))
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := state.file.Write(state.path); err != nil {
		return result, input(err)
	}
	return result, nil
}

// EnvPush applies the local file to the remote scope. Creation and update go
// through one bulk request; deletion happens only with Prune, one variable at
// a time by identity. A key whose remote value is withheld is overwritten only
// with Force, because nothing local can prove it differs.
func (a *App) EnvPush(ctx context.Context, options EnvPushOptions, confirm ConfirmPush) (EnvPushResult, error) {
	state, err := a.envState(ctx, options.EnvOptions)
	if err != nil {
		return EnvPushResult{}, err
	}
	if !state.file.Exists {
		return EnvPushResult{}, input(fmt.Errorf("%s does not exist; nothing to push", state.path))
	}
	var diff EnvDiffResult
	classify(state, &diff)
	plan := EnvPushPlan{Target: targetInfo(state.session.project), Scope: state.scope, File: state.path, Create: diff.Added, Update: diff.Changed}
	for _, key := range diff.Withheld {
		local, present := state.file.Get(key)
		if !present {
			continue
		}
		if options.Force {
			plan.Update = append(plan.Update, EnvChange{Key: key, Local: local})
		} else {
			plan.Skipped = append(plan.Skipped, key)
		}
	}
	if options.Prune {
		plan.Delete = diff.Removed
	}
	result := EnvPushResult{Plan: plan, Warnings: state.session.warnings}
	if len(plan.Skipped) > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("%d withheld value(s) were not pushed; use --force to overwrite them: %s", len(plan.Skipped), strings.Join(plan.Skipped, ", ")))
	}
	if !options.Prune && len(diff.Removed) > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("%d remote-only key(s) were left in place; use --prune to delete them", len(diff.Removed)))
	}
	if plan.Empty() {
		return result, nil
	}
	if !options.Yes {
		if confirm == nil {
			return result, input(errors.New("pushing variables requires --yes when input is noninteractive"))
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
	items := make([]models.EnvironmentVariableInput, 0, len(plan.Create)+len(plan.Update))
	for _, change := range plan.Create {
		items = append(items, variableInput(change, options.Preview, nil))
	}
	for _, change := range plan.Update {
		remote := state.remote[change.Key]
		items = append(items, variableInput(change, options.Preview, &remote))
	}
	if err := state.session.backend.UpsertEnvironmentVariables(ctx, state.session.project.Application.UUID, items); err != nil {
		return result, fmt.Errorf("push %d variable(s): %w", len(items), err)
	}
	for _, change := range plan.Delete {
		variable := state.remote[change.Key]
		if err := state.session.backend.DeleteEnvironmentVariable(ctx, state.session.project.Application.UUID, variable.UUID); err != nil {
			return result, fmt.Errorf("delete %s: %w", change.Key, err)
		}
	}
	result.Warnings = append(result.Warnings, "Variables change on the next deployment; run coolship deploy to apply them.")
	return result, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// variableInput builds one upsert item. An update restates the flags the
// server would otherwise reset; a creation takes server defaults, except that a
// value spanning lines is declared multiline so it survives rendering.
func variableInput(change EnvChange, preview bool, remote *models.EnvironmentVariable) models.EnvironmentVariableInput {
	item := models.EnvironmentVariableInput{Key: change.Key, Value: change.Local, IsPreview: preview}
	multiline := strings.Contains(change.Local, "\n")
	if remote != nil {
		literal, shownOnce := remote.IsLiteral, remote.IsShownOnce
		multiline = multiline || remote.IsMultiline
		item.IsLiteral, item.IsMultiline, item.IsShownOnce = &literal, &multiline, &shownOnce
		return item
	}
	if multiline {
		item.IsMultiline = &multiline
	}
	return item
}
