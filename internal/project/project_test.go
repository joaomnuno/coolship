package project_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/joaomnuno/coolship/internal/config"
	"github.com/joaomnuno/coolship/internal/project"
)

const valid = `# This comment must survive unchanged linking.
version = 1
[project]
context = "home"
project = "Personal"
environment = "production"
application = "web"
`

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func discover(t *testing.T, paths project.Paths, allowMissing bool) project.Project {
	t.Helper()
	value, err := project.Discover(paths, allowMissing)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestDiscoveryNearestConfigurationAndGitBoundary(t *testing.T) {
	outer := t.TempDir()
	write(t, filepath.Join(outer, "coolship.toml"), valid)
	repo := filepath.Join(outer, "repo")
	nested := filepath.Join(repo, "apps", "web", "src")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	before, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := project.Discover(project.Paths{CWD: nested}, false); !errors.Is(err, project.ErrNotLinked) {
		t.Fatalf("must not discover parent repository binding: %v", err)
	}
	missing := discover(t, project.Paths{CWD: nested}, true)
	if missing.ConfigRoot != repo || missing.GitRoot != repo || missing.Exists {
		t.Fatalf("unexpected missing project: %#v", missing)
	}
	write(t, filepath.Join(repo, "coolship.toml"), valid)
	found := discover(t, project.Paths{CWD: nested}, false)
	if found.ConfigRoot != repo || found.Fingerprint == "" {
		t.Fatalf("expected Git root config: %#v", found)
	}
	app := filepath.Join(repo, "apps", "web")
	write(t, filepath.Join(app, "coolship.toml"), valid)
	found = discover(t, project.Paths{CWD: nested}, false)
	if found.ConfigRoot != app || found.GitRoot != repo {
		t.Fatalf("expected nearest config and independent Git root: %#v", found)
	}
	after, err := os.Getwd()
	if err != nil || before != after {
		t.Fatalf("process working directory changed: %q -> %q (%v)", before, after, err)
	}
}

func TestDiscoveryWorktreeAndSymlinkPaths(t *testing.T) {
	outer := t.TempDir()
	write(t, filepath.Join(outer, "coolship.toml"), valid)
	worktree := filepath.Join(outer, "worktree")
	write(t, filepath.Join(worktree, ".git"), "gitdir: ../main/.git/worktrees/example\n")
	write(t, filepath.Join(worktree, "nested", "keep"), "")
	alias := filepath.Join(outer, "alias")
	if err := os.Symlink(worktree, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	value := discover(t, project.Paths{CWD: filepath.Join(alias, "nested")}, true)
	if value.GitRoot != worktree || value.ConfigRoot != worktree || value.Exists {
		t.Fatalf("worktree boundary must use canonical path: %#v", value)
	}
	write(t, filepath.Join(worktree, "coolship.toml"), valid)
	value = discover(t, project.Paths{CWD: filepath.Join(alias, "nested")}, false)
	if value.ConfigRoot != worktree {
		t.Fatalf("unexpected symlink discovery: %#v", value)
	}
}

func TestExplicitConfigResolvesAgainstEffectiveDirectory(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "coolship.toml"), valid)
	write(t, filepath.Join(root, "nested", "custom.toml"), valid)
	value := discover(t, project.Paths{CWD: filepath.Join(root, "nested"), ConfigPath: "custom.toml"}, false)
	if value.ConfigPath != filepath.Join(root, "nested", "custom.toml") {
		t.Fatalf("unexpected config path: %s", value.ConfigPath)
	}
	if _, err := project.Discover(project.Paths{CWD: root, ConfigPath: "missing.toml"}, false); !errors.Is(err, project.ErrNotLinked) {
		t.Fatalf("explicit missing config must not fall back: %v", err)
	}
	value = discover(t, project.Paths{CWD: root, ConfigPath: "missing.toml"}, true)
	if value.Exists || value.ConfigPath != filepath.Join(root, "missing.toml") {
		t.Fatalf("unexpected explicit creation plan: %#v", value)
	}
}

func TestTargetRootAndEnvironmentOverride(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "coolship.toml"), valid+"root = 'apps/web'\n")
	if err := os.MkdirAll(filepath.Join(root, "apps", "web"), 0755); err != nil {
		t.Fatal(err)
	}
	value := discover(t, project.Paths{CWD: root}, false)
	target, err := project.Select(value, "", "staging")
	if err != nil {
		t.Fatal(err)
	}
	if target.Key != "default" || target.AppRoot != filepath.Join(root, "apps", "web") || target.Binding.Environment != "staging" || value.Config.Project.Environment != "production" {
		t.Fatalf("unexpected target or mutated source: %#v %#v", target, value)
	}
	for _, pin := range []string{"environment", "application"} {
		t.Run(pin, func(t *testing.T) {
			pinned := value
			if pin == "environment" {
				pinned.Config.Project.EnvironmentUUID = "environment-id"
			} else {
				pinned.Config.Project.ApplicationUUID = "application-id"
			}
			if _, err := project.Select(pinned, "", "staging"); !errors.Is(err, config.ErrInvalid) {
				t.Fatalf("override must reject conflicting pin: %v", err)
			}
			if _, err := project.Select(pinned, "", "production"); err != nil {
				t.Fatalf("same-environment override should retain pin: %v", err)
			}
		})
	}
}

func TestTargetRejectsSymlinkEscape(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	write(t, filepath.Join(root, "coolship.toml"), valid+"root = 'escape'\n")
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	value := discover(t, project.Paths{CWD: root}, false)
	if _, err := project.Select(value, "", ""); !errors.Is(err, config.ErrInvalid) {
		t.Fatalf("symlink root must stay within config directory: %v", err)
	}
	if err := project.WriteBinding(value, "", value.Config.Project, true); !errors.Is(err, config.ErrInvalid) {
		t.Fatalf("writing must validate root containment: %v", err)
	}
}

func TestWritePreservesUnchangedAndRequiresReviewedReplacement(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "coolship.toml")
	write(t, path, valid)
	value := discover(t, project.Paths{CWD: root}, false)
	if err := project.WriteBinding(value, "", value.Config.Project, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != valid {
		t.Fatalf("unchanged binding lost comments: %q (%v)", data, err)
	}
	binding := value.Config.Project
	binding.Application = "other"
	if err := project.WriteBinding(value, "", binding, false); !errors.Is(err, project.ErrReplacementRequired) {
		t.Fatalf("different binding needs explicit replacement: %v", err)
	}
	if err := project.WriteBinding(value, "", binding, true); err != nil {
		t.Fatal(err)
	}
	updated := discover(t, project.Paths{CWD: root}, false)
	if updated.Config.Project.Application != "other" {
		t.Fatalf("replacement missing: %#v", updated.Config)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("replacement changed permissions: %v (%v)", info, err)
	}
	if err := project.WriteBinding(value, "", binding, true); !errors.Is(err, project.ErrConflict) {
		t.Fatalf("stale reviewed fingerprint must fail: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary files remain: %v (%v)", entries, err)
	}
}

func TestWriteNeverClobbersConcurrentCreationOrEdits(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "creation", true: "replacement"}[existing], func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "coolship.toml")
			if existing {
				write(t, path, valid)
			}
			value := discover(t, project.Paths{CWD: root, ConfigPath: "coolship.toml"}, true)
			binding := config.Binding{Project: "Personal", Environment: "production", Application: "other", Root: "."}
			write(t, path, valid+"# concurrent editor\n")
			if err := project.WriteBinding(value, "", binding, true); !errors.Is(err, project.ErrConflict) {
				t.Fatalf("concurrent change must fail: %v", err)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != valid+"# concurrent editor\n" {
				t.Fatalf("concurrent content overwritten: %q (%v)", data, err)
			}
		})
	}
}

func TestConcurrentCreatorsPublishOnlyOneCompleteBinding(t *testing.T) {
	root := t.TempDir()
	value := discover(t, project.Paths{CWD: root, ConfigPath: "coolship.toml"}, true)
	start := make(chan struct{})
	errorsSeen := make(chan error, 2)
	var workers sync.WaitGroup
	for _, name := range []string{"first", "second"} {
		workers.Go(func() {
			<-start
			errorsSeen <- project.WriteBinding(value, "", config.Binding{Project: "Personal", Environment: "production", Application: name}, false)
		})
	}
	close(start)
	workers.Wait()
	close(errorsSeen)
	successes := 0
	for err := range errorsSeen {
		if err == nil {
			successes++
		} else if !errors.Is(err, project.ErrConflict) {
			t.Fatalf("unexpected competing creation error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("expected one successful creator, got %d", successes)
	}
	value = discover(t, project.Paths{CWD: root}, false)
	if value.Config.Project.Application != "first" && value.Config.Project.Application != "second" {
		t.Fatalf("published incomplete configuration: %#v", value.Config)
	}
}

// TestLinkedAgreesWithProposeReviewWithoutTheFinishedBinding checks that
// Linked's cheaper answer, reached without a binding to compare, agrees with
// the review decision Propose reaches once it has one.
func TestLinkedAgreesWithProposeReviewWithoutTheFinishedBinding(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	unlinked, err := project.Discover(project.Paths{CWD: root}, true)
	if err != nil {
		t.Fatal(err)
	}
	if project.Linked(unlinked, "") || project.Linked(unlinked, "web") {
		t.Fatal("a directory with no configuration is never already linked")
	}

	named := namedProject(t, "apps/api")
	if !project.Linked(named, "api") {
		t.Fatal("an existing named target must be linked")
	}
	if project.Linked(named, "worker") {
		t.Fatal("a new named target is not linked; Propose adds it without review")
	}
	if !project.Linked(named, "") || !project.Linked(named, "default") {
		t.Fatal("the single form conflicts with an existing named configuration")
	}
	if plan, err := project.Propose(named, "default", config.Binding{Project: "Personal", Environment: "production", Application: "worker", Root: "."}); err != nil || !plan.Review {
		t.Fatalf("Propose must review the same conversion: plan=%+v err=%v", plan, err)
	}
	if plan, err := project.Propose(named, "worker", config.Binding{Project: "Personal", Environment: "production", Application: "worker", Root: "apps/worker"}); err != nil || plan.Review {
		t.Fatalf("Propose must not review a genuinely new target: plan=%+v err=%v", plan, err)
	}

	single := namedProject(t, ".")
	single.Config = config.Config{Version: 1, Project: config.Binding{Project: "Personal", Environment: "production", Application: "app", Root: "."}}
	single.Exists = true
	if !project.Linked(single, "") || !project.Linked(single, "default") {
		t.Fatal("an existing single-form binding is linked")
	}
	if !project.Linked(single, "web") {
		t.Fatal("a named target conflicts with an existing single-form configuration")
	}
}

func namedProject(t *testing.T, cwd string) project.Project {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"apps/web", "apps/api", "apps/api/src", "tools"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	data := []byte(`version = 1

[apps.web]
project = "Personal"
environment = "production"
application = "frontend"
root = "apps/web"

[apps.api]
project = "Personal"
environment = "production"
application = "backend"
root = "apps/api"

[apps.docs]
project = "Personal"
environment = "production"
application = "docs"
root = "apps/web"
`)
	if err := os.WriteFile(filepath.Join(root, "coolship.toml"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	value, err := project.Discover(project.Paths{CWD: filepath.Join(root, cwd)}, false)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestNamedTargetSelection(t *testing.T) {
	for _, test := range []struct {
		cwd, target, want, wantErr string
	}{
		{"apps/api", "", "api", ""},
		{"apps/api/src", "", "api", ""},             // deepest containing root
		{"apps/web", "", "", "share the same root"}, // web and docs both own apps/web
		{"apps/web", "docs", "docs", ""},
		{"", "", "", "no target's root contains"},
		{"tools", "", "", "no target's root contains"},
		{"", "web", "web", ""},
		{"", "nope", "", `no target named "nope"; available: api, docs, web`},
	} {
		t.Run(test.cwd+"/"+test.target, func(t *testing.T) {
			value := namedProject(t, test.cwd)
			target, err := project.Select(value, test.target, "")
			if test.wantErr != "" {
				if !errors.Is(err, config.ErrInvalid) || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("err=%v, want %q", err, test.wantErr)
				}
				return
			}
			if err != nil || target.Key != test.want || !strings.HasSuffix(target.AppRoot, filepath.FromSlash(value.Config.Apps[test.want].Root)) {
				t.Fatalf("target=%+v err=%v", target, err)
			}
		})
	}
	single := namedProject(t, "")
	single.Config = config.Config{Version: 1, Project: config.Binding{Project: "P", Environment: "E", Application: "A", Root: "."}}
	if _, err := project.Select(single, "web", ""); !errors.Is(err, config.ErrInvalid) {
		t.Fatalf("target on single form: %v", err)
	}
	if target, err := project.Select(single, "default", ""); err != nil || target.Key != "default" {
		t.Fatalf("default on single form: %+v %v", target, err)
	}
}

func TestProposeAddsTargetsWithoutReviewButReviewsChangesAndConversions(t *testing.T) {
	value := namedProject(t, "apps/api")
	fresh := config.Binding{Project: "Personal", Environment: "production", Application: "worker", Root: "apps/worker"}
	if err := os.MkdirAll(filepath.Join(value.ConfigRoot, "apps/worker"), 0o755); err != nil {
		t.Fatal(err)
	}
	plan, err := project.Propose(value, "worker", fresh)
	if err != nil || plan.Review || plan.Unchanged || len(plan.Config.Apps) != 4 {
		t.Fatalf("adding a target: plan=%+v err=%v", plan, err)
	}
	same := value.Config.Apps["api"]
	plan, err = project.Propose(value, "api", same)
	if err != nil || !plan.Unchanged {
		t.Fatalf("identical target: plan=%+v err=%v", plan, err)
	}
	changed := same
	changed.Application = "backend-v2"
	plan, err = project.Propose(value, "api", changed)
	if err != nil || !plan.Review || plan.Unchanged {
		t.Fatalf("changed target: plan=%+v err=%v", plan, err)
	}
	plan, err = project.Propose(value, "", changed)
	if err != nil || !plan.Review || plan.Config.Named() {
		t.Fatalf("conversion to single form: plan=%+v err=%v", plan, err)
	}
	if _, err := project.Propose(value, "bad name", fresh); !errors.Is(err, config.ErrInvalid) {
		t.Fatal("invalid target name accepted")
	}
	if plan, err := project.Propose(value, "default", fresh); err != nil || plan.Config.Named() || !plan.Review {
		t.Fatalf("\"default\" must mean the single form and review the conversion: plan=%+v err=%v", plan, err)
	}
	// Writing follows the plan: adding needs no replace; changing does.
	if err := project.WriteBinding(value, "worker", fresh, false); err != nil {
		t.Fatalf("add without replace: %v", err)
	}
	value, _ = project.Discover(project.Paths{CWD: value.CWD}, false)
	if len(value.Config.Apps) != 4 || value.Config.Apps["worker"].Application != "worker" {
		t.Fatalf("worker not added: %+v", value.Config.Apps)
	}
	if err := project.WriteBinding(value, "api", changed, false); !errors.Is(err, project.ErrReplacementRequired) {
		t.Fatalf("change without replace: %v", err)
	}
	if err := project.WriteBinding(value, "api", changed, true); err != nil {
		t.Fatalf("change with replace: %v", err)
	}
	if err := project.WriteBinding(value, "", changed, true); err == nil {
		// Stale fingerprint after the previous write: conversion must be a conflict, not a clobber.
		t.Fatal("stale project converted the file")
	} else if !errors.Is(err, project.ErrConflict) {
		t.Fatalf("stale write: %v", err)
	}
	if root := project.DefaultRoot(value, "api"); root != "apps/api" {
		t.Fatalf("DefaultRoot for a target from apps/api = %q", root)
	}
	if root := project.DefaultRoot(value, ""); root != "." {
		t.Fatalf("DefaultRoot for the single form = %q", root)
	}
}
