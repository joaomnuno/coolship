package ui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/joaomnuno/coolship/internal/service"
)

// TestInit_DeployKeySuggestsRepoOverride checks that the follow-up command
// printed after --create-deploy-key carries --repo, so re-running it targets
// the repository the key was registered against instead of whatever the git
// origin remote happens to say (issue #42).
func TestInit_DeployKeySuggestsRepoOverride(t *testing.T) {
	var out bytes.Buffer
	renderer := NewRenderer(Streams{Out: &out}, "")
	result := service.InitResult{
		Plan: service.InitPlan{Instance: "homelab"},
		DeployKey: &service.DeployKeyResult{
			Name:       "coolship-deploy",
			UUID:       "key-uuid",
			PublicKey:  "ssh-ed25519 AAAA... coolship",
			Repository: "git@github.com:acme/widgets.git",
		},
	}
	if err := renderer.Init(result); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	want := "coolship init --source deploy-key --deploy-key coolship-deploy --repo git@github.com:acme/widgets.git\n"
	if !strings.Contains(out.String(), want) {
		t.Errorf("Init() output = %q; want it to contain %q", out.String(), want)
	}
}

// TestInit_DeployKeySuggestsRepoOverride_Quoting checks that a repository or
// key name with shell metacharacters is quoted so the printed command can be
// copied and run verbatim without splitting into extra arguments.
func TestInit_DeployKeySuggestsRepoOverride_Quoting(t *testing.T) {
	var out bytes.Buffer
	renderer := NewRenderer(Streams{Out: &out}, "")
	result := service.InitResult{
		Plan: service.InitPlan{Instance: "homelab"},
		DeployKey: &service.DeployKeyResult{
			Name:       "my key",
			UUID:       "key-uuid",
			PublicKey:  "ssh-ed25519 AAAA... coolship",
			Repository: "git@github.com:acme/it's-widgets.git",
		},
	}
	if err := renderer.Init(result); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	want := `coolship init --source deploy-key --deploy-key 'my key' --repo 'git@github.com:acme/it'\''s-widgets.git'` + "\n"
	if !strings.Contains(out.String(), want) {
		t.Errorf("Init() output = %q; want it to contain %q", out.String(), want)
	}
}

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"":                       "''",
		"git@github.com:a/b.git": "git@github.com:a/b.git",
		"coolship-deploy":        "coolship-deploy",
		"my key":                 "'my key'",
		"it's":                   `'it'\''s'`,
		"$(rm -rf /)":            `'$(rm -rf /)'`,
	}
	for input, want := range cases {
		if got := shellQuote(input); got != want {
			t.Errorf("shellQuote(%q) = %q; want %q", input, got, want)
		}
	}
}
