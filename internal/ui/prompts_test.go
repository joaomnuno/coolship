package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/joaomnuno/coolship/internal/service"
)

// TestSelectNamesTheFlagsWhenNoninteractive checks the hint given for every
// kind of choice a workflow asks for, so a kind the switch does not know is
// noticed here rather than as "the corresponding link flags" on a terminal.
func TestSelectNamesTheFlagsWhenNoninteractive(t *testing.T) {
	prompter := NewPrompter(Streams{})
	for kind, flags := range map[string]string{
		"context": "--context", "instance": "--context",
		"project":     "--project or --project-uuid",
		"environment": "--environment or --environment-uuid",
		"application": "--application or --application-uuid",
		"server":      "--server",
		"source":      "--source",
		"GitHub App":  "--github-app",
		"deploy key":  "--deploy-key or --create-deploy-key",
	} {
		_, err := prompter.Select(context.Background(), kind, []service.Choice{{ID: "one", Name: "one"}, {ID: "two", Name: "two"}})
		want := "select " + kind + " explicitly with " + flags + " when input is noninteractive"
		if !errors.Is(err, service.ErrInput) || err.Error() != want {
			t.Errorf("Select(%q) = %v; want %q", kind, err, want)
		}
	}
	if _, err := prompter.Select(context.Background(), "widget", nil); err == nil || !strings.Contains(err.Error(), "the corresponding link flags") {
		t.Errorf("unknown kind: %v", err)
	}
}

// TestConfirmInitPrintsWarningsBeforeTheQuestion keeps the warnings where they
// can still change the answer, above the question rather than after creation.
func TestConfirmInitPrintsWarningsBeforeTheQuestion(t *testing.T) {
	var diagnostic strings.Builder
	prompter := NewPrompter(Streams{In: strings.NewReader("n\n"), Err: &diagnostic, Interactive: true})
	plan := service.InitPlan{Name: "web", Instance: "home", Project: "Personal", Environment: "production", Server: "localhost",
		Repository: "https://github.com/o/r", Branch: "main", BuildPack: "dockercompose", Path: "coolship.toml",
		Warnings: []string{"No service has a domain yet."}}
	accepted, err := prompter.ConfirmInit(context.Background(), plan)
	if err != nil || accepted {
		t.Fatalf("accepted=%v err=%v", accepted, err)
	}
	out := diagnostic.String()
	warning := strings.Index(out, "No service has a domain yet.")
	question := strings.Index(out, "Create application web")
	if warning < 0 || question < 0 || warning > question {
		t.Fatalf("warning at %d, question at %d in:\n%s", warning, question, out)
	}
}
