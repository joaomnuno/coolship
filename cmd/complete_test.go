package cmd_test

import (
	"strings"
	"testing"

	"github.com/joaomnuno/coolship/internal/service"
)

// complete runs the hidden command a shell's completion script calls, and
// returns the candidate lines and the trailing directive line.
func complete(t *testing.T, args ...string) ([]string, string) {
	t.Helper()
	return completeWith(t, fakeApplication{}, args...)
}

func completeWith(t *testing.T, app fakeApplication, args ...string) ([]string, string) {
	t.Helper()
	out, _, err := execute(t, app, append([]string{"__complete"}, args...)...)
	if err != nil {
		t.Fatalf("__complete %v: %v", args, err)
	}
	// Stdout is the candidates, one per line, then the directive; the
	// "Completion ended with" line Cobra writes goes to stderr.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) == 0 {
		t.Fatalf("__complete %v produced nothing", args)
	}
	return lines[:len(lines)-1], lines[len(lines)-1]
}

// noFileComp is the directive that tells a shell to offer nothing rather than
// fall back to its own file list.
const noFileComp = ":4"

func TestCompletionNeverFallsBackToTheFileList(t *testing.T) {
	// Every command a developer can type, with the argument it takes. None of
	// them should leave the shell listing the files in the directory (#100).
	for _, command := range [][]string{
		{"status"}, {"deploy"}, {"logs"}, {"open"}, {"stop"}, {"start"}, {"restart"},
		{"preview"}, {"deployments"}, {"cancel"}, {"domain"}, {"domain", "set"},
		{"init"}, {"link"}, {"unlink"}, {"doctor"}, {"login"}, {"logout"}, {"alias"},
		{"config"}, {"config", "show"}, {"config", "get"}, {"config", "set"},
		{"env"}, {"env", "pull"}, {"env", "diff"}, {"env", "push"},
	} {
		name := strings.Join(command, " ")
		_, directive := complete(t, append(command, "")...)
		if directive != noFileComp {
			t.Errorf("%s -> %s, want %s", name, directive, noFileComp)
		}
	}
}

func TestCompletionOffersTheNamedTargetsOfTheConfiguration(t *testing.T) {
	app := fakeApplication{targets: func(service.Options) []service.CompletionTarget {
		return []service.CompletionTarget{
			{Name: "api", Purpose: "Personal / production / api-service"},
			{Name: "web", Purpose: "Personal / staging / web-frontend"},
		}
	}}
	for _, args := range [][]string{{"deploy", ""}, {"logs", ""}, {"deploy", "--target", ""}} {
		lines, directive := completeWith(t, app, args...)
		if directive != noFileComp {
			t.Errorf("%v -> %s", args, directive)
		}
		if len(lines) != 2 || lines[0] != "api\tPersonal / production / api-service" || lines[1] != "web\tPersonal / staging / web-frontend" {
			t.Errorf("%v -> %q", args, lines)
		}
	}
	// A prefix narrows the list, as it does for command names.
	lines, _ := completeWith(t, app, "deploy", "w")
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "web\t") {
		t.Errorf("prefixed -> %q", lines)
	}
	// cancel's second argument is a deployment UUID, which only the server
	// knows, so nothing is offered for it.
	lines, directive := completeWith(t, app, "cancel", "api", "")
	if len(lines) != 0 || directive != noFileComp {
		t.Errorf("cancel's UUID -> %q %s", lines, directive)
	}
	// A single-target project has no name to complete.
	empty := fakeApplication{targets: func(service.Options) []service.CompletionTarget { return nil }}
	lines, directive = completeWith(t, empty, "deploy", "")
	if len(lines) != 0 || directive != noFileComp {
		t.Errorf("single target -> %q %s", lines, directive)
	}
}

func TestCompletionOffersPreferenceKeysAndTheirValues(t *testing.T) {
	lines, directive := complete(t, "config", "get", "")
	if directive != noFileComp {
		t.Fatalf("directive %s", directive)
	}
	var names []string
	for _, line := range lines {
		name, description, found := strings.Cut(line, "\t")
		if !found || description == "" {
			t.Errorf("key without a description: %q", line)
		}
		names = append(names, name)
	}
	for _, want := range []string{"verbosity", "build_logs", "color", "hints", "update_check"} {
		if !contains(names, want) {
			t.Errorf("%s is missing from %v", want, names)
		}
	}
	// config set completes the key first, then the values that key accepts.
	lines, _ = complete(t, "config", "set", "verbosity", "")
	if strings.Join(lines, " ") != "normal verbose debug" {
		t.Errorf("verbosity values -> %q", lines)
	}
	lines, _ = complete(t, "config", "set", "hints", "")
	if strings.Join(lines, " ") != "true false" {
		t.Errorf("hints values -> %q", lines)
	}
	// A prefix narrows the values, and an unknown key offers none.
	lines, _ = complete(t, "config", "set", "color", "a")
	if strings.Join(lines, " ") != "auto always" {
		t.Errorf("prefixed color values -> %q", lines)
	}
	lines, directive = complete(t, "config", "set", "nonsense", "")
	if len(lines) != 0 || directive != noFileComp {
		t.Errorf("unknown key -> %q %s", lines, directive)
	}
}

func TestCompletionOffersSavedContextsAndFixedFlagValues(t *testing.T) {
	app := fakeApplication{savedContexts: func(string) []service.SavedContext {
		return []service.SavedContext{
			{Name: "home", URL: "https://coolify.example.com", Default: true},
			{Name: "work", URL: "https://coolify.work.example"},
		}
	}}
	for _, args := range [][]string{{"logout", ""}, {"status", "--context", ""}} {
		lines, directive := completeWith(t, app, args...)
		if directive != noFileComp {
			t.Errorf("%v -> %s", args, directive)
		}
		if len(lines) != 2 || lines[0] != "home\thttps://coolify.example.com (default)" || lines[1] != "work\thttps://coolify.work.example" {
			t.Errorf("%v -> %q", args, lines)
		}
	}
	// --format is a closed list this binary already knows.
	lines, _ := complete(t, "status", "--format", "")
	if strings.Join(lines, " ") != "human json" {
		t.Errorf("--format -> %q", lines)
	}
	// So are init's build packs and sources, which its help documents.
	lines, _ = complete(t, "init", "--build-pack", "")
	if strings.Join(lines, " ") != "railpack nixpacks static dockerfile dockercompose" {
		t.Errorf("--build-pack -> %q", lines)
	}
	lines, _ = complete(t, "init", "--source", "")
	if strings.Join(lines, " ") != "auto public github-app deploy-key" {
		t.Errorf("--source -> %q", lines)
	}
}

func TestCompletionKeepsTheFileListWhereAPathIsMeant(t *testing.T) {
	// dev runs a local command, so the shell's own list is the right answer.
	_, directive := complete(t, "dev", "")
	if directive == noFileComp {
		t.Errorf("dev should keep file completion, got %s", directive)
	}
	// --cwd names a directory, so only directories are offered.
	_, directive = complete(t, "status", "--cwd", "")
	if directive != ":16" {
		t.Errorf("--cwd -> %s, want the directory filter :16", directive)
	}
	// A dotenv path keeps the file list.
	_, directive = complete(t, "env", "pull", "--file", "")
	if directive == noFileComp {
		t.Errorf("--file should keep file completion, got %s", directive)
	}
}

func TestCompletionAsksTheServerForNothing(t *testing.T) {
	// A completion that reached the network would hang a Tab on an unreachable
	// instance. The fake fails every remote call, so any use is a failure here.
	app := fakeApplication{}
	for _, args := range [][]string{
		{"deploy", ""}, {"status", ""}, {"logout", ""}, {"cancel", ""},
		{"link", "--project", ""}, {"link", "--application", ""},
		{"status", "--environment", ""}, {"init", "--github-app", ""},
	} {
		if _, _, err := execute(t, app, append([]string{"__complete"}, args...)...); err != nil {
			t.Errorf("__complete %v reached something it should not: %v", args, err)
		}
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
