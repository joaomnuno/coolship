package gitinfo

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
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
		if got, err := SSHRemote(bad); err == nil {
			t.Errorf("SSHRemote(%q) = %q, want an error", bad, got)
		}
	}
	// Credentials in a URL are dropped, never carried into the request.
	if got, err := NormalizeRemote("https://user:secret@github.com/owner/repo"); err != nil || got != "https://github.com/owner/repo" {
		t.Fatalf("credentials: %q %v", got, err)
	}
	if got, err := SSHRemote("https://user:secret@github.com/owner/repo"); err != nil || got != "git@github.com:owner/repo.git" {
		t.Fatalf("credentials: %q %v", got, err)
	}
}

func TestSSHRemoteKeepsSSHFormsAndConvertsHTTPS(t *testing.T) {
	for _, test := range []struct{ in, want string }{
		{"git@github.com:owner/repo.git", "git@github.com:owner/repo.git"},
		{"git@github.com:owner/repo", "git@github.com:owner/repo.git"},
		{"deploy@GitLab.com:group/sub/repo.git", "deploy@gitlab.com:group/sub/repo.git"},
		{"ssh://git@github.com/owner/repo.git", "git@github.com:owner/repo.git"},
		{"ssh://git@gitea.example.com:2222/owner/repo", "git@gitea.example.com:2222/owner/repo.git"},
		{"ssh://gitea.example.com/owner/repo", "git@gitea.example.com:owner/repo.git"},
		{"https://github.com/owner/repo", "git@github.com:owner/repo.git"},
		{"https://github.com/owner/repo.git/", "git@github.com:owner/repo.git"},
		{"http://gitea.example.com/owner/repo", "git@gitea.example.com:owner/repo.git"},
	} {
		got, err := SSHRemote(test.in)
		if err != nil || got != test.want {
			t.Errorf("SSHRemote(%q) = %q, %v; want %q", test.in, got, err, test.want)
		}
	}
	repository, err := Parse("ssh://git@github.com:22/owner/repo")
	if err != nil || repository != (Repository{Remote: "https://github.com/owner/repo", SSH: "git@github.com:22/owner/repo.git"}) {
		t.Fatalf("Parse: %+v %v", repository, err)
	}
}

func TestNameHostAndPath(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/owner/repo": "repo", "https://github.com/owner/repo.git/": "repo", "repo": "repo",
	} {
		if got := Name(in); got != want {
			t.Errorf("Name(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string][2]string{
		"https://github.com/Owner/Repo":     {"github.com", "Owner/Repo"},
		"git@GitHub.com:owner/repo.git":     {"github.com", "owner/repo"},
		"ssh://git@gitlab.com:22/a/b/c.git": {"gitlab.com", "a/b/c"},
		"not a remote":                      {"", ""},
	} {
		if got := [2]string{Host(in), Path(in)}; got != want {
			t.Errorf("Host/Path(%q) = %v, want %v", in, got, want)
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
	if err != nil || repository != (Repository{Remote: "https://github.com/owner/repo", SSH: "git@github.com:owner/repo.git", Branch: "feature"}) {
		t.Fatalf("repository=%+v err=%v", repository, err)
	}
	git("-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "one")
	git("checkout", "-q", "--detach")
	if _, err := Inspect(context.Background(), dir); err == nil || !strings.Contains(err.Error(), "detached") {
		t.Fatalf("detached HEAD: %v", err)
	}
	// Heads reads a remote the way an anonymous client would; a local
	// repository served by file:// stands in for a public host.
	git("checkout", "-q", "feature")
	git("branch", "-q", "other")
	heads, err := Heads(context.Background(), "file://"+dir)
	if err != nil || !reflect.DeepEqual(heads, []string{"feature", "other"}) {
		t.Fatalf("heads=%v err=%v", heads, err)
	}
	if _, err := Heads(context.Background(), "file://"+dir+"/missing"); err == nil || !strings.Contains(err.Error(), "git") {
		t.Fatalf("missing remote: %v", err)
	}
	for _, bad := range []string{dir, "--upload-pack=touch", "-c"} {
		if _, err := Heads(context.Background(), bad); err == nil {
			t.Fatalf("Heads(%q) accepted a non-URL", bad)
		}
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Heads(cancelled, "file://"+dir); err == nil {
		t.Fatal("cancelled probe succeeded")
	}
}

// TestHeadsIgnoresStoredLogins puts a login where git would find one on its
// own — ~/.netrc, an http.extraHeader in the environment, a repository named
// by GIT_DIR — and checks that the probe never presents it: a server that
// demands credentials must see none, and the repository must not look public.
func TestHeadsIgnoresStoredLogins(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	var mu sync.Mutex
	var authorized []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			mu.Lock()
			authorized = append(authorized, r.URL.Path)
			mu.Unlock()
		}
		w.Header().Set("WWW-Authenticate", `Basic realm="probe"`)
		http.Error(w, "credentials required", http.StatusUnauthorized)
	}))
	defer server.Close()
	header := "Authorization: Basic dXNlcjpzZWNyZXQ="
	for name, login := range map[string]func(t *testing.T){
		"netrc": func(t *testing.T) {
			home := t.TempDir()
			if err := os.WriteFile(filepath.Join(home, ".netrc"), []byte("machine 127.0.0.1 login user password secret\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOME", home)
		},
		"config in the environment": func(t *testing.T) {
			t.Setenv("GIT_CONFIG_COUNT", "1")
			t.Setenv("GIT_CONFIG_KEY_0", "http.extraHeader")
			t.Setenv("GIT_CONFIG_VALUE_0", header)
		},
		"repository named by GIT_DIR": func(t *testing.T) {
			dir := t.TempDir()
			for _, args := range [][]string{{"init", "-q"}, {"config", "http.extraHeader", header}} {
				command := exec.Command("git", args...)
				command.Dir = dir
				if out, err := command.CombinedOutput(); err != nil {
					t.Fatalf("git %v: %v\n%s", args, err, out)
				}
			}
			t.Setenv("GIT_DIR", filepath.Join(dir, ".git"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			login(t)
			mu.Lock()
			authorized = nil
			mu.Unlock()
			if heads, err := Heads(context.Background(), server.URL+"/owner/repo"); err == nil {
				t.Fatalf("a repository behind a login listed %v", heads)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(authorized) != 0 {
				t.Fatalf("the probe presented credentials to %v", authorized)
			}
		})
	}
}

// TestHeadsReturnsWhenCancelledMidRequest cancels while git-remote-https is
// waiting on a server that never answers; the probe must return with the
// cancellation rather than wait for the connection to time out.
func TestHeadsReturnsWhenCancelledMidRequest(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	requested := make(chan struct{}, 1)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case requested <- struct{}{}:
		default:
		}
		<-release
		http.NotFound(w, nil)
	}))
	defer server.Close()
	defer close(release) // lets the handler finish before the server closes
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := Heads(ctx, server.URL+"/owner/repo")
		done <- err
	}()
	select {
	case <-requested:
	case <-time.After(30 * time.Second):
		t.Fatal("git never reached the server")
	}
	started := time.Now()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled probe returned %v", err)
		}
		if elapsed := time.Since(started); elapsed > 10*time.Second {
			t.Fatalf("cancellation took %s", elapsed)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the probe did not return after cancellation")
	}
}
