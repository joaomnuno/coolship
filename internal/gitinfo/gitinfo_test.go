package gitinfo

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestNormalizeRemote(t *testing.T) {
	for _, test := range []struct{ in, want string }{
		{"https://github.com/owner/repo", "https://github.com/owner/repo"},
		{"https://github.com/owner/repo.git", "https://github.com/owner/repo"},
		{"https://GitHub.com/owner/repo/", "https://github.com/owner/repo"},
		{"http://gitea.example.com/owner/repo.git", "https://gitea.example.com/owner/repo"},
		{"git@github.com:owner/repo.git", "https://github.com/owner/repo"},
		{"git@gitlab.com:group/sub/repo.git", "https://gitlab.com/group/sub/repo"},
		{"ssh://git@github.com/owner/repo.git", "https://github.com/owner/repo"},
		{"ssh://git@github.com:22/owner/repo", "https://github.com/owner/repo"},
		{"git://github.com/owner/repo.git", "https://github.com/owner/repo"},
		{"  git@github.com:owner/repo  ", "https://github.com/owner/repo"},
	} {
		got, err := NormalizeRemote(test.in)
		if err != nil || got != test.want {
			t.Errorf("NormalizeRemote(%q) = %q, %v; want %q", test.in, got, err, test.want)
		}
	}
	for _, bad := range []string{"", "repo", "/srv/git/repo.git", "../repo", "ftp://example.com/owner/repo",
		"https://github.com/repo", "https://github.com/owner/repo?x=1", "https://github.com/owner/../repo",
		"git@github.com:owner/re po", "https://user:secret@github.com/own\ner/repo"} {
		if got, err := NormalizeRemote(bad); err == nil {
			t.Errorf("NormalizeRemote(%q) = %q, want an error", bad, got)
		} else if strings.Contains(err.Error(), "secret") {
			t.Errorf("error exposes credentials: %v", err)
		}
	}
	// Credentials in a URL are dropped, never carried into the request.
	if got, err := NormalizeRemote("https://user:secret@github.com/owner/repo"); err != nil || got != "https://github.com/owner/repo" {
		t.Errorf("credentials: %q %v", got, err)
	}
}

func TestName(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/owner/repo": "repo", "https://github.com/owner/repo.git/": "repo", "repo": "repo",
	} {
		if got := Name(in); got != want {
			t.Errorf("Name(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInspectReadsRemoteAndBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = dir
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "feature")
	if _, err := Inspect(context.Background(), dir); err == nil || !strings.Contains(err.Error(), "origin") {
		t.Fatalf("no remote: %v", err)
	}
	git("remote", "add", "origin", "git@github.com:owner/repo.git")
	repository, err := Inspect(context.Background(), dir)
	if err != nil || repository.Remote != "https://github.com/owner/repo" || repository.Branch != "feature" {
		t.Fatalf("repository=%+v err=%v", repository, err)
	}
	git("-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "one")
	git("checkout", "-q", "--detach")
	if _, err := Inspect(context.Background(), dir); err == nil || !strings.Contains(err.Error(), "detached") {
		t.Fatalf("detached HEAD: %v", err)
	}
}
