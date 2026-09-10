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
