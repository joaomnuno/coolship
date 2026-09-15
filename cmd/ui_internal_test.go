package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/joaomnuno/coolship/internal/project"
	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
	"github.com/spf13/cobra"
)

// menuApp answers Status and nothing else; any other workflow panics, which
// is how a test would notice the menu running something it should not.
type menuApp struct {
	Application
	status func(context.Context, service.Options) (service.StatusResult, error)
}

func (a menuApp) Status(ctx context.Context, options service.Options) (service.StatusResult, error) {
	return a.status(ctx, options)
}

// captureMenu runs `coolship ui` with args, with a menu that records what it
// was given and chooses choice.
func captureMenu(t *testing.T, app Application, choice []string, args ...string) (ui.MenuOptions, string, error) {
	t.Helper()
	var captured ui.MenuOptions
	var out bytes.Buffer
	root := NewRootCommand(app, ui.Streams{Out: &out}, "test", withMenuRunner(func(_ context.Context, _ ui.Streams, options ui.MenuOptions) ([]string, error) {
		captured = options
		return choice, nil
	}))
	root.SetArgs(append([]string{"ui"}, args...))
	err := root.ExecuteContext(context.Background())
	return captured, out.String(), err
}

// helpSections reads the grouped command names from `coolship help`.
func helpSections(t *testing.T) ([]string, map[string][]string) {
	t.Helper()
	var out bytes.Buffer
	root := NewRootCommand(nil, ui.Streams{Out: &out}, "test")
	root.SetArgs([]string{"help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	titles := []string{}
	commands := map[string][]string{}
	current := ""
	text := out.String()
	usage := strings.Index(text, "Usage:")
	flags := strings.Index(text, "\nFlags:")
	if usage < 0 || flags < usage {
		t.Fatalf("help has no Usage and Flags sections: %s", text)
	}
	for _, line := range strings.Split(text[usage:flags], "\n") {
		switch {
		case line == "" || strings.HasSuffix(line, ":"):
			current = ""
		case !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "Use "):
			current = line
			titles = append(titles, line)
		case current != "" && strings.HasPrefix(line, "  "):
			commands[current] = append(commands[current], strings.Fields(line)[0])
		}
	}
	return titles, commands
}

// The menu is experimental, so the help says so where it lists it: under
// Maintain, with a summary that starts with the marker.
func TestHelpMarksTheMenuExperimentalUnderMaintain(t *testing.T) {
	var out bytes.Buffer
	root := NewRootCommand(nil, ui.Streams{Out: &out}, "test")
	root.SetArgs([]string{"help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	maintain := strings.Index(out.String(), "\nMaintain\n")
	if maintain < 0 {
		t.Fatalf("help has no Maintain group: %s", out.String())
	}
	if !regexp.MustCompile(`\n  ui +\(experimental\) `).MatchString(out.String()[maintain:]) {
		t.Fatalf("help does not list ui under Maintain as (experimental): %s", out.String())
	}
}

func TestMenuListsTheHelpGroupsInTheirOrder(t *testing.T) {
	options, _, err := captureMenu(t, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	titles, commands := helpSections(t)
	if len(titles) != 5 {
		t.Fatalf("help titles = %q", titles)
	}
	if len(options.Groups) != len(titles) {
		t.Fatalf("menu has %d groups; help has %q", len(options.Groups), titles)
	}
	for i, group := range options.Groups {
		if group.Title != titles[i] {
			t.Fatalf("group %d = %q; help says %q", i, group.Title, titles[i])
		}
		var tops []string
		for _, verb := range group.Verbs {
			if len(tops) == 0 || tops[len(tops)-1] != verb.Path[0] {
				tops = append(tops, verb.Path[0])
			}
		}
		var want []string
		for _, name := range commands[group.Title] {
			if name != "ui" && name != "help" && name != "completion" {
				want = append(want, name)
			}
		}
		if strings.Join(tops, " ") != strings.Join(want, " ") {
			t.Errorf("%s: menu lists %q; help lists %q", group.Title, tops, want)
		}
	}
	names := map[string]ui.MenuVerb{}
	for _, group := range options.Groups {
		for _, verb := range group.Verbs {
			names[verb.Name()] = verb
		}
	}
	for _, name := range []string{"env pull", "env diff", "env push", "domain", "domain set"} {
		if _, ok := names[name]; !ok {
			t.Errorf("menu lacks %q", name)
		}
	}
	if _, ok := names["env"]; ok {
		t.Error("menu lists env, which only prints help")
	}
}

// TestMenuAsksForEveryRequiredArgument keeps menuInputs in step with the
// command tree: a verb whose Use line names a required argument must ask
// for it, and every entry must name a verb the menu lists.
func TestMenuAsksForEveryRequiredArgument(t *testing.T) {
	options, _, err := captureMenu(t, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	root := NewRootCommand(nil, ui.Streams{}, "test")
	listed := map[string]bool{}
	for _, group := range options.Groups {
		for _, verb := range group.Verbs {
			listed[verb.Name()] = true
			command, _, err := root.Find(verb.Path)
			if err != nil {
				t.Fatal(err)
			}
			if required := requiredArguments(command); len(required) > 0 && len(verb.Inputs) == 0 {
				t.Errorf("%s requires %q but the menu does not ask for it", verb.Name(), required)
			}
		}
	}
	for name := range menuInputs {
		if !listed[name] {
			t.Errorf("menuInputs names %q, which the menu does not list", name)
		}
	}
	if len(menuInputs["preview"]) == 0 || menuInputs["preview"][0].Flag != "pr" {
		t.Error("preview does not ask for --pr")
	}
}

// optionalArguments matches a bracketed part of a Use line, such as
// [target] or [-- command...].
var optionalArguments = regexp.MustCompile(`\[[^\]]*\]`)

func requiredArguments(command *cobra.Command) []string {
	return strings.Fields(optionalArguments.ReplaceAllString(command.Use, ""))[1:]
}

func TestMenuRunsTheChosenVerbWithTheSameGlobalFlags(t *testing.T) {
	var seen []service.Options
	app := menuApp{status: func(_ context.Context, options service.Options) (service.StatusResult, error) {
		seen = append(seen, options)
		return service.StatusResult{Target: service.TargetInfo{Application: "web"}, Status: "running:healthy"}, nil
	}}
	var out bytes.Buffer
	root := NewRootCommand(app, ui.Streams{Out: &out}, "test", withMenuRunner(func(ctx context.Context, _ ui.Streams, options ui.MenuOptions) ([]string, error) {
		// The header reads with the invocation's overrides.
		if _, err := options.Status(ctx); err != nil {
			return nil, err
		}
		return []string{"status"}, nil
	}))
	root.SetArgs([]string{"ui", "--format", "json", "--context", "staging", "-e", "preview"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 {
		t.Fatalf("status ran %d times; want the header read and the verb", len(seen))
	}
	for _, options := range seen {
		if options.Context != "staging" || options.Environment != "preview" {
			t.Errorf("status options = %+v", options)
		}
	}
	var result service.StatusResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || result.Status != "running:healthy" {
		t.Fatalf("verb output is not the status JSON: %q (%v)", out.String(), err)
	}
}

// logoutApp answers Logout and records the options it was given.
type logoutApp struct {
	Application
	seen *service.LogoutOptions
}

func (a logoutApp) Logout(_ context.Context, options service.LogoutOptions) (service.LogoutResult, error) {
	*a.seen = options
	return service.LogoutResult{Name: options.Name}, nil
}

// A positional answer that starts with a dash stays a value: the menu puts
// it after "--", and the global flags go in front of that boundary.
func TestMenuKeepsADashedAnswerPositional(t *testing.T) {
	var seen service.LogoutOptions
	var out bytes.Buffer
	root := NewRootCommand(logoutApp{seen: &seen}, ui.Streams{Out: &out}, "test", withMenuRunner(func(context.Context, ui.Streams, ui.MenuOptions) ([]string, error) {
		return []string{"logout", "--", "-x"}, nil
	}))
	root.SetArgs([]string{"ui", "--coolify-config", "/tmp/coolify.json"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if seen.Name != "-x" || seen.ConfigPath != "/tmp/coolify.json" {
		t.Fatalf("logout options = %+v", seen)
	}
}

func TestWithGlobalFlags(t *testing.T) {
	flags := []string{"--context=home"}
	for _, tc := range []struct{ args, want []string }{
		{[]string{"status"}, []string{"status", "--context=home"}},
		{[]string{"preview", "--pr=4"}, []string{"preview", "--pr=4", "--context=home"}},
		{[]string{"domain", "set", "--", "-a.example.com"}, []string{"domain", "set", "--context=home", "--", "-a.example.com"}},
	} {
		if got := withGlobalFlags(tc.args, flags); strings.Join(got, " ") != strings.Join(tc.want, " ") {
			t.Errorf("withGlobalFlags(%q) = %q; want %q", tc.args, got, tc.want)
		}
	}
}

func TestMenuLeftWithoutChoosingRunsNothing(t *testing.T) {
	_, out, err := captureMenu(t, nil, nil)
	if err != nil || out != "" {
		t.Fatalf("out = %q, err = %v", out, err)
	}
}

func TestMenuRefusesWithoutATerminal(t *testing.T) {
	var out, diagnostic bytes.Buffer
	root := NewRootCommand(nil, ui.Streams{Out: &out, Err: &diagnostic}, "test")
	root.SetArgs([]string{"ui"})
	err := root.ExecuteContext(context.Background())
	if ui.ExitCode(err) != 2 || !strings.Contains(err.Error(), "coolship help") {
		t.Fatalf("err = %v, exit %d", err, ui.ExitCode(err))
	}
}

func TestMenuRecognizesAnUnlinkedDirectory(t *testing.T) {
	options, _, err := captureMenu(t, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !options.NotLinked(&service.InputError{Err: project.ErrNotLinked}) || options.NotLinked(errors.New("unreachable")) {
		t.Fatal("NotLinked does not tell an unlinked directory from a failure")
	}
}
