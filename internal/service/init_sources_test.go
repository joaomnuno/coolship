package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/joaomnuno/coolship/internal/auth"
	"github.com/joaomnuno/coolship/internal/models"
	"github.com/joaomnuno/coolship/internal/sshkey"
)

func TestInitSourceAutoProbesTheRemote(t *testing.T) {
	t.Run("reachable remote is public", func(t *testing.T) {
		f := newBackend()
		app, _, _ := testApp(f)
		result, err := app.Init(context.Background(), InitOptions{Options: Options{CWD: unlinkedDirectory(t)}, Project: "Personal", Yes: true}, nil, nil, nil)
		if err != nil || result.Plan.Source != "" || result.Plan.GitHubApp != "" || result.Plan.DeployKey != "" || len(result.Warnings) != 0 {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		if f.created[0].Source != SourcePublic || f.created[0].GitHubAppUUID != "" || f.created[0].PrivateKeyUUID != "" || f.created[0].GitRepository != "https://github.com/owner/new-app" {
			t.Fatalf("created %+v", f.created[0])
		}
		// The plan of a public application serializes as it did before sources existed.
		data, _ := json.Marshal(result.Plan)
		if strings.Contains(string(data), "source") || strings.Contains(string(data), "deploy_key") {
			t.Fatalf("public plan JSON carries source fields: %s", data)
		}
	})
	t.Run("branch missing from the remote is a warning", func(t *testing.T) {
		f := newBackend()
		app, _, _ := testApp(f)
		result, err := app.Init(context.Background(), InitOptions{Options: Options{CWD: unlinkedDirectory(t)}, Project: "Personal", Branch: "unpushed", Yes: true}, nil, nil, nil)
		if err != nil || len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], `"unpushed"`) || f.calls["create-application"] != 1 {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	})
	t.Run("private remote needs a source when noninteractive", func(t *testing.T) {
		f := newBackend()
		f.heads = nil
		app, _, _ := testApp(f)
		_, err := app.Init(context.Background(), InitOptions{Options: Options{CWD: unlinkedDirectory(t)}, Project: "Personal", Yes: true}, nil, nil, nil)
		if !errors.Is(err, ErrInput) || !strings.Contains(err.Error(), "--source github-app or --source deploy-key") || !strings.Contains(err.Error(), "terminal prompts disabled") || f.calls["create-application"] != 0 {
			t.Fatalf("err=%v calls=%v", err, f.calls)
		}
		// A remote off GitHub can only take a deploy key.
		f.repository.Remote, f.repository.SSH = "https://gitlab.com/owner/new-app", "git@gitlab.com:owner/new-app.git"
		_, err = app.Init(context.Background(), InitOptions{Options: Options{CWD: unlinkedDirectory(t)}, Project: "Personal", Yes: true}, nil, nil, nil)
		if !errors.Is(err, ErrInput) || strings.Contains(err.Error(), "github-app") || !strings.Contains(err.Error(), "--source deploy-key") {
			t.Fatalf("gitlab: %v", err)
		}
	})
	t.Run("private remote asks which source to use", func(t *testing.T) {
		f := newBackend()
		f.heads = nil
		app, _, _ := testApp(f)
		var prompts []string
		selector := func(_ context.Context, kind string, choices []Choice) (string, error) {
			ids := make([]string, len(choices))
			for i, choice := range choices {
				ids[i] = choice.ID
			}
			prompts = append(prompts, kind+":"+strings.Join(ids, ","))
			switch kind {
			case "source", "deploy key":
				return choices[0].ID, nil // the GitHub App when offered, else the only source; the first key
			}
			return "", errors.New("unexpected prompt " + kind)
		}
		result, err := app.Init(context.Background(), InitOptions{Options: Options{CWD: unlinkedDirectory(t)}, Project: "Personal", Yes: true}, selector, nil, nil)
		if err != nil || !reflect.DeepEqual(prompts, []string{"source:github-app,deploy-key"}) {
			t.Fatalf("err=%v prompts=%v", err, prompts)
		}
		// The only installed app was taken without a prompt, then checked.
		if result.Plan.Source != SourceGitHubApp || result.Plan.GitHubApp != "docs-app" || f.created[0].GitHubAppUUID != "gh-docs" || f.created[0].Source != SourceGitHubApp ||
			f.calls["github-branches"] != 1 || len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "not reachable anonymously") {
			t.Fatalf("result=%+v created=%+v calls=%v", result, f.created[0], f.calls)
		}
		// Off GitHub, the deploy key is the only source, and it is still asked
		// for, as is the only usable key: neither is taken implicitly.
		f.repository.Remote, f.repository.SSH = "https://gitlab.com/owner/new-app", "git@gitlab.com:owner/new-app.git"
		prompts = nil
		result, err = app.Init(context.Background(), InitOptions{Options: Options{CWD: unlinkedDirectory(t)}, Project: "Personal", Name: "second", Yes: true}, selector, nil, nil)
		if err != nil || !reflect.DeepEqual(prompts, []string{"source:deploy-key", "deploy key:key-deploy"}) || result.Plan.Source != SourceDeployKey || result.Plan.DeployKey != "deploy" || result.Plan.Repository != "git@gitlab.com:owner/new-app.git" ||
			f.created[1].Source != SourceDeployKey || f.created[1].PrivateKeyUUID != "key-deploy" || f.created[1].GitRepository != "git@gitlab.com:owner/new-app.git" {
			t.Fatalf("result=%+v created=%+v prompts=%v err=%v", result, f.created[1], prompts, err)
		}
	})
	t.Run("explicit public skips the probe", func(t *testing.T) {
		f := newBackend()
		f.heads = nil // an unreachable remote would otherwise refuse, or hold the command
		app, _, _ := testApp(f)
		result, err := app.Init(context.Background(), InitOptions{Options: Options{CWD: unlinkedDirectory(t)}, Project: "Personal", Source: SourcePublic, Branch: "unpushed", Yes: true}, nil, nil, nil)
		if err != nil || len(result.Warnings) != 0 || len(f.probed) != 0 || f.created[0].Source != SourcePublic || f.created[0].GitBranch != "unpushed" {
			t.Fatalf("result=%+v probed=%v err=%v", result, f.probed, err)
		}
	})
	t.Run("without git the source must be named", func(t *testing.T) {
		f := newBackend()
		bare := New(Dependencies{NewBackend: func(auth.Credentials) (Backend, error) { return f, nil },
			ResolveCredentials: func(auth.Options) (auth.Credentials, error) {
				return auth.Credentials{Name: "home", URL: "https://coolify.example.com", Token: "t"}, nil
			}})
		options := InitOptions{Options: Options{CWD: unlinkedDirectory(t)}, Project: "Personal", Repository: "https://github.com/owner/new-app", Branch: "main", Yes: true}
		if _, err := bare.Init(context.Background(), options, nil, nil, nil); !errors.Is(err, ErrInput) || !strings.Contains(err.Error(), "pass --source") {
			t.Fatalf("no prober: %v", err)
		}
		options.Source = SourcePublic
		if result, err := bare.Init(context.Background(), options, nil, nil, nil); err != nil || len(result.Warnings) != 0 {
			t.Fatalf("explicit public without prober: %+v %v", result, err)
		}
	})
}

func TestInitGitHubAppIsCheckedBeforeCreation(t *testing.T) {
	newOptions := func(t *testing.T, extra InitOptions) InitOptions {
		extra.Options, extra.Project, extra.Yes = Options{CWD: unlinkedDirectory(t)}, "Personal", true
		return extra
	}
	t.Run("named app clones the checked repository", func(t *testing.T) {
		f := newBackend()
		f.heads = nil // never consulted: the flag settles the source
		app, _, _ := testApp(f)
		result, err := app.Init(context.Background(), newOptions(t, InitOptions{GitHubApp: "docs-app"}), nil, nil, nil)
		if err != nil || result.Plan.Source != SourceGitHubApp || result.Plan.GitHubApp != "docs-app" || result.Plan.Repository != "https://github.com/owner/new-app" || len(f.probed) != 0 {
			t.Fatalf("result=%+v probed=%v err=%v", result, f.probed, err)
		}
		if f.created[0].Source != SourceGitHubApp || f.created[0].GitHubAppUUID != "gh-docs" || f.created[0].PrivateKeyUUID != "" || f.created[0].GitRepository != "https://github.com/owner/new-app" {
			t.Fatalf("created %+v", f.created[0])
		}
	})
	for _, test := range []struct {
		name    string
		options InitOptions
		want    string
	}{
		{"unknown app", InitOptions{GitHubApp: "nope"}, "docs-app"},
		{"repository the app cannot see", InitOptions{GitHubApp: "docs-app", Repository: "https://github.com/owner/other", Branch: "main"}, "cannot access owner/other"},
		{"branch missing from a complete listing", InitOptions{GitHubApp: "docs-app", Branch: "nope"}, `branch "nope" does not exist`},
		{"host that is not GitHub", InitOptions{Source: SourceGitHubApp, Repository: "https://gitlab.com/owner/repo", Branch: "main"}, "github.com only"},
		{"app flag with another source", InitOptions{Source: SourceDeployKey, GitHubApp: "docs-app"}, "--github-app applies to --source github-app"},
		{"unknown source", InitOptions{Source: "svn"}, "--source must be one of"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newBackend()
			app, _, _ := testApp(f)
			_, err := app.Init(context.Background(), newOptions(t, test.options), nil, nil, nil)
			if !errors.Is(err, ErrInput) || !strings.Contains(err.Error(), test.want) || f.calls["create-application"] != 0 {
				t.Fatalf("err=%v calls=%v", err, f.calls)
			}
		})
	}
	t.Run("branch beyond the first page is left to the deployment", func(t *testing.T) {
		// GitHub answers 30 branches per page and Coolify relays the first
		// page only, so a full page proves nothing about a branch it lacks.
		f := newBackend()
		var page []models.GitHubBranch
		for i := 0; i < githubBranchPage; i++ {
			page = append(page, models.GitHubBranch{Name: fmt.Sprintf("feature/%02d", i)})
		}
		f.branches["1 owner/new-app"] = page
		app, _, _ := testApp(f)
		result, err := app.Init(context.Background(), newOptions(t, InitOptions{GitHubApp: "docs-app"}), nil, nil, nil)
		if err != nil || len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], `Branch "main" is not among the first 30 branches`) || f.created[0].GitBranch != "main" || f.created[0].GitHubAppUUID != "gh-docs" {
			t.Fatalf("full page: result=%+v err=%v", result, err)
		}
		// One branch fewer is a complete listing, and the typo is refused.
		f.branches["1 owner/new-app"] = page[1:]
		if _, err := app.Init(context.Background(), newOptions(t, InitOptions{GitHubApp: "docs-app", Name: "second"}), nil, nil, nil); !errors.Is(err, ErrInput) || !strings.Contains(err.Error(), `branch "main" does not exist`) || !strings.Contains(err.Error(), "feature/08, …") || f.calls["create-application"] != 1 {
			t.Fatalf("complete listing: %v", err)
		}
	})
	t.Run("a listing fault is not an access refusal", func(t *testing.T) {
		f := newBackend()
		f.branchError = statusError{code: 502}
		app, _, _ := testApp(f)
		_, err := app.Init(context.Background(), newOptions(t, InitOptions{GitHubApp: "docs-app"}), nil, nil, nil)
		if err == nil || errors.Is(err, ErrInput) || !strings.Contains(err.Error(), "list the branches of owner/new-app") || f.calls["create-application"] != 0 {
			t.Fatalf("branch listing fault: %v calls=%v", err, f.calls)
		}
	})
	t.Run("repository names match case-insensitively", func(t *testing.T) {
		f := newBackend()
		app, _, _ := testApp(f)
		result, err := app.Init(context.Background(), newOptions(t, InitOptions{GitHubApp: "docs-app", Repository: "https://github.com/owner/secret", Branch: "main"}), nil, nil, nil)
		if err != nil || result.Plan.Name != "secret" || f.created[0].GitHubAppUUID != "gh-docs" {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	})
	t.Run("prompts among installed apps", func(t *testing.T) {
		f := newBackend()
		f.githubApps = append(f.githubApps, models.GitHubApp{ID: 2, UUID: "gh-other", Name: "other-app"})
		f.branches["2 owner/new-app"] = f.branches["1 owner/new-app"]
		app, _, _ := testApp(f)
		var choices []Choice
		selector := func(_ context.Context, kind string, offered []Choice) (string, error) {
			if kind != "GitHub App" {
				return "", errors.New("unexpected prompt " + kind)
			}
			choices = offered
			return "gh-other", nil
		}
		result, err := app.Init(context.Background(), newOptions(t, InitOptions{Source: SourceGitHubApp}), selector, nil, nil)
		if err != nil || len(choices) != 2 || choices[0].Name != "docs-app" || choices[0].Detail != "Org" || choices[1].Detail != "personal installation" || result.Plan.GitHubApp != "other-app" || f.created[0].GitHubAppUUID != "gh-other" {
			t.Fatalf("result=%+v choices=%+v err=%v", result, choices, err)
		}
		// Noninteractive with two apps is an input error before any check.
		if _, err := app.Init(context.Background(), newOptions(t, InitOptions{Source: SourceGitHubApp}), nil, nil, nil); !errors.Is(err, ErrInput) || f.calls["github-branches"] != 1 {
			t.Fatalf("ambiguous app: %v calls=%v", err, f.calls)
		}
	})
	t.Run("system-wide app of another team is checked by the server", func(t *testing.T) {
		f := newBackend()
		f.githubApps = append(f.githubApps, models.GitHubApp{ID: 9, UUID: "gh-wide", Name: "wide", IsSystemWide: true, TeamID: 5})
		app, _, _ := testApp(f)
		result, err := app.Init(context.Background(), newOptions(t, InitOptions{GitHubApp: "wide"}), nil, nil, nil)
		if err != nil || len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "another team") || f.created[0].GitHubAppUUID != "gh-wide" || f.calls["github-branches"] != 1 {
			t.Fatalf("result=%+v err=%v calls=%v", result, err, f.calls)
		}
	})
	t.Run("no installed app", func(t *testing.T) {
		f := newBackend()
		f.githubApps = f.githubApps[:1]
		app, _, _ := testApp(f)
		if _, err := app.Init(context.Background(), newOptions(t, InitOptions{Source: SourceGitHubApp}), nil, nil, nil); !errors.Is(err, ErrInput) || !strings.Contains(err.Error(), "--source deploy-key") {
			t.Fatalf("no apps: %v", err)
		}
	})
	t.Run("declined plan creates nothing", func(t *testing.T) {
		f := newBackend()
		app, _, _ := testApp(f)
		var plan InitPlan
		declined := func(_ context.Context, p InitPlan) (bool, error) { plan = p; return false, nil }
		options := newOptions(t, InitOptions{GitHubApp: "docs-app"})
		options.Yes = false
		if _, err := app.Init(context.Background(), options, nil, declined, nil); !errors.Is(err, ErrCancelled) || f.calls["create-application"] != 0 || plan.GitHubApp != "docs-app" || plan.Source != SourceGitHubApp {
			t.Fatalf("err=%v calls=%v plan=%+v", err, f.calls, plan)
		}
	})
}

func TestInitDeployKeys(t *testing.T) {
	newOptions := func(t *testing.T, extra InitOptions) InitOptions {
		extra.Options, extra.Project, extra.Yes = Options{CWD: unlinkedDirectory(t)}, "Personal", true
		return extra
	}
	t.Run("named key clones over SSH", func(t *testing.T) {
		f := newBackend()
		app, _, _ := testApp(f)
		result, err := app.Init(context.Background(), newOptions(t, InitOptions{DeployKey: "deploy"}), nil, nil, nil)
		if err != nil || result.Plan.Source != SourceDeployKey || result.Plan.DeployKey != "deploy" || result.Plan.Repository != "git@github.com:owner/new-app.git" || result.Plan.NewDeployKey || len(f.probed) != 0 {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		if f.created[0].Source != SourceDeployKey || f.created[0].PrivateKeyUUID != "key-deploy" || f.created[0].GitHubAppUUID != "" || f.created[0].GitRepository != "git@github.com:owner/new-app.git" || f.calls["create-key"] != 0 {
			t.Fatalf("created %+v", f.created[0])
		}
		// An SSH remote given explicitly is kept as written.
		result, err = app.Init(context.Background(), newOptions(t, InitOptions{DeployKey: "deploy", Repository: "ssh://git@gitea.example.com:2222/owner/app", Branch: "main"}), nil, nil, nil)
		if err != nil || result.Plan.Repository != "git@gitea.example.com:2222/owner/app.git" || f.created[1].GitRepository != "git@gitea.example.com:2222/owner/app.git" {
			t.Fatalf("ssh remote: %+v %v", result, err)
		}
		// Any key can be named, even one the prompt would not offer.
		if result, err := app.Init(context.Background(), newOptions(t, InitOptions{DeployKey: "github-app-docs", Name: "third"}), nil, nil, nil); err != nil || f.created[2].PrivateKeyUUID != "key-app" {
			t.Fatalf("git-related key: %+v %v", result, err)
		}
	})
	t.Run("unknown key names the available ones", func(t *testing.T) {
		f := newBackend()
		app, _, _ := testApp(f)
		_, err := app.Init(context.Background(), newOptions(t, InitOptions{DeployKey: "nope"}), nil, nil, nil)
		if !errors.Is(err, ErrInput) || !strings.Contains(err.Error(), "available: deploy)") || !strings.Contains(err.Error(), `--create-deploy-key "nope"`) || f.calls["create-application"] != 0 {
			t.Fatalf("err=%v calls=%v", err, f.calls)
		}
	})
	t.Run("prompt leaves out the localhost and app keys", func(t *testing.T) {
		f := newBackend()
		f.keys = append(f.keys, models.PrivateKey{ID: 3, UUID: "key-other", Name: "other", Fingerprint: "f3"})
		app, _, _ := testApp(f)
		var choices []Choice
		selector := func(_ context.Context, kind string, offered []Choice) (string, error) {
			if kind != "deploy key" {
				return "", errors.New("unexpected prompt " + kind)
			}
			choices = offered
			return offered[len(offered)-1].ID, nil
		}
		result, err := app.Init(context.Background(), newOptions(t, InitOptions{Source: SourceDeployKey}), selector, nil, nil)
		if err != nil || len(choices) != 2 || choices[0].Name != "deploy" || choices[1].Name != "other" || choices[1].Detail != "f3" || result.Plan.DeployKey != "other" || f.created[0].PrivateKeyUUID != "key-other" {
			t.Fatalf("result=%+v choices=%+v err=%v", result, choices, err)
		}
		// A single usable key is still asked for, and needs --deploy-key when
		// there is nobody to ask.
		f.keys, choices = f.keys[:3], nil
		if result, err := app.Init(context.Background(), newOptions(t, InitOptions{Source: SourceDeployKey, Name: "second"}), selector, nil, nil); err != nil || len(choices) != 1 || choices[0].Name != "deploy" || result.Plan.DeployKey != "deploy" {
			t.Fatalf("single key: result=%+v choices=%+v err=%v", result, choices, err)
		}
		if _, err := app.Init(context.Background(), newOptions(t, InitOptions{Source: SourceDeployKey}), nil, nil, nil); !errors.Is(err, ErrInput) || !strings.Contains(err.Error(), "--deploy-key NAME") || !strings.Contains(err.Error(), "among deploy") || f.calls["create-application"] != 2 {
			t.Fatalf("single key without a prompt: %v calls=%v", err, f.calls)
		}
		f.keys = f.keys[:2]
		if _, err := app.Init(context.Background(), newOptions(t, InitOptions{Source: SourceDeployKey}), selector, nil, nil); !errors.Is(err, ErrInput) || !strings.Contains(err.Error(), "--create-deploy-key NAME") {
			t.Fatalf("no usable key: %v", err)
		}
	})
	t.Run("creating a key stops before the application", func(t *testing.T) {
		f := newBackend()
		app, _, _ := testApp(f)
		dir := unlinkedDirectory(t)
		options := InitOptions{Options: Options{CWD: dir}, Project: "Personal", CreateDeployKey: "fresh", Deploy: true}
		var plan InitPlan
		declined := func(_ context.Context, p InitPlan) (bool, error) { plan = p; return false, nil }
		if _, err := app.Init(context.Background(), options, nil, declined, nil); !errors.Is(err, ErrCancelled) || f.calls["create-key"] != 0 {
			t.Fatalf("declined: err=%v calls=%v", err, f.calls)
		}
		if !plan.NewDeployKey || plan.DeployKey != "fresh" || plan.Source != SourceDeployKey || plan.Repository != "git@github.com:owner/new-app.git" {
			t.Fatalf("plan %+v", plan)
		}
		accepted := func(context.Context, InitPlan) (bool, error) { return true, nil }
		result, err := app.Init(context.Background(), options, nil, accepted, nil)
		if err != nil || result.DeployKey == nil || f.calls["create-key"] != 1 || f.calls["create-application"] != 0 || f.calls["deploy"] != 0 {
			t.Fatalf("result=%+v err=%v calls=%v", result, err, f.calls)
		}
		if *result.DeployKey != (DeployKeyResult{Name: "fresh", UUID: "key-fresh", PublicKey: "ssh-ed25519 PUBLIC fresh", Repository: "git@github.com:owner/new-app.git"}) || result.Target.ApplicationUUID != "" {
			t.Fatalf("key result %+v", result)
		}
		if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "--deploy has no effect") {
			t.Fatalf("warnings %v", result.Warnings)
		}
		// The private half went to Coolify and nowhere else.
		if len(f.createdKeys) != 1 || !strings.Contains(f.createdKeys[0], "SECRET-fresh") {
			t.Fatalf("keys sent %q", f.createdKeys)
		}
		if data, _ := json.Marshal(result); strings.Contains(string(data), "SECRET") || strings.Contains(string(data), "PRIVATE KEY") {
			t.Fatalf("result carries the private key: %s", data)
		}
		if _, err := os.Stat(filepath.Join(dir, "coolship.toml")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("key creation wrote configuration")
		}
		// The key now exists, so the same name is refused and the key is usable.
		if _, err := app.Init(context.Background(), options, nil, accepted, nil); !errors.Is(err, ErrInput) || !strings.Contains(err.Error(), "already holds") {
			t.Fatalf("duplicate: %v", err)
		}
		if result, err := app.Init(context.Background(), InitOptions{Options: Options{CWD: dir}, Project: "Personal", DeployKey: "fresh", Yes: true}, nil, nil, nil); err != nil || f.created[0].PrivateKeyUUID != "key-fresh" || result.DeployKey != nil {
			t.Fatalf("use created key: %+v %v", result, err)
		}
	})
	t.Run("key creation failures", func(t *testing.T) {
		f := newBackend()
		f.keyError = statusError{code: 422}
		app, _, _ := testApp(f)
		if _, err := app.Init(context.Background(), newOptions(t, InitOptions{CreateDeployKey: "fresh"}), nil, nil, nil); err == nil || errors.Is(err, ErrInput) {
			t.Fatalf("listing failure: %v", err)
		}
		f.keyError = nil
		f.keys = nil
		g := newBackend()
		app, _, _ = testApp(g)
		g.keyError = nil
		g.keys = nil
		called := 0
		app = New(Dependencies{NewBackend: func(auth.Credentials) (Backend, error) { return g, nil },
			ResolveCredentials: func(auth.Options) (auth.Credentials, error) {
				return auth.Credentials{Name: "home", URL: "https://coolify.example.com", Token: "t"}, nil
			},
			GenerateKey: func(string) (sshkey.Pair, error) { called++; return sshkey.Pair{}, errors.New("no entropy") }})
		if _, err := app.Init(context.Background(), newOptions(t, InitOptions{CreateDeployKey: "fresh", Repository: "https://github.com/owner/new-app", Branch: "main"}), nil, nil, nil); err == nil || !strings.Contains(err.Error(), "generate deploy key") || called != 1 || g.calls["create-key"] != 0 {
			t.Fatalf("generation failure: %v calls=%v", err, g.calls)
		}
	})
	for _, test := range []struct {
		name    string
		options InitOptions
		want    string
	}{
		{"both key flags", InitOptions{DeployKey: "deploy", CreateDeployKey: "fresh"}, "cannot be combined"},
		{"key flag with another source", InitOptions{Source: SourceGitHubApp, DeployKey: "deploy"}, "apply to --source deploy-key"},
		{"bad key name", InitOptions{CreateDeployKey: "-bad"}, "deploy key name"},
		{"existing key name", InitOptions{CreateDeployKey: "deploy"}, "already holds"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newBackend()
			app, _, _ := testApp(f)
			_, err := app.Init(context.Background(), newOptions(t, test.options), nil, nil, nil)
			if !errors.Is(err, ErrInput) || !strings.Contains(err.Error(), test.want) || f.calls["create-key"] != 0 || f.calls["create-application"] != 0 {
				t.Fatalf("err=%v calls=%v", err, f.calls)
			}
		})
	}
}
