package preferences

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func TestKeysDescribeEveryFieldOfTheFile(t *testing.T) {
	want := []string{"verbosity", "build_logs", "color", "hints", "update_check"}
	if got := Names(); !slices.Equal(got, want) {
		t.Fatalf("Names() = %q; want %q", got, want)
	}
	for _, key := range Keys() {
		if key.Description == "" || !slices.Contains(key.Allowed, key.Default) {
			t.Errorf("%s: description %q, default %q not among %q", key.Name, key.Description, key.Default, key.Allowed)
		}
		// Every allowed value round-trips through the file.
		for _, value := range key.Allowed {
			path := filepath.Join(t.TempDir(), "preferences.toml")
			if err := Apply(path, []Change{{key.Name, value}}); err != nil {
				t.Fatalf("Apply(%s=%s) = %v", key.Name, value, err)
			}
			prefs, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if got, _, _ := prefs.Get(key.Name); got != value {
				t.Errorf("%s=%s read back as %q", key.Name, value, got)
			}
		}
	}
}

func TestGetReportsDefaultsAndWhatTheFileSets(t *testing.T) {
	off := false
	prefs := Preferences{Verbosity: VerbosityDebug, Hints: &off}
	for _, tt := range []struct {
		name, want string
		set        bool
	}{
		{"verbosity", "debug", true}, {"hints", "false", true},
		{"build_logs", "auto", false}, {"color", "auto", false}, {"update_check", "true", false},
	} {
		got, set, err := prefs.Get(tt.name)
		if err != nil || got != tt.want || set != tt.set {
			t.Errorf("Get(%s) = %q, %v, %v; want %q, %v", tt.name, got, set, err, tt.want, tt.set)
		}
	}
	var keyErr *KeyError
	if _, _, err := prefs.Get("colour"); !errors.As(err, &keyErr) {
		t.Fatalf("Get(unknown) = %v", err)
	}
	if !(Preferences{}).HintsEnabled() || prefs.HintsEnabled() {
		t.Fatal("HintsEnabled: absent must be on, false must be off")
	}
}

func TestApplyCreatesTheFilePrivately(t *testing.T) {
	path := filepath.Join(t.TempDir(), "coolship", "preferences.toml")
	if err := Apply(path, []Change{{"hints", "false"}, {"verbosity", "verbose"}}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "hints = false\nverbosity = \"verbose\"\n" {
		t.Fatalf("content = %q", data)
	}
	if runtime.GOOS != "windows" {
		file, _ := os.Stat(path)
		dir, _ := os.Stat(filepath.Dir(path))
		if file.Mode().Perm() != 0o600 || dir.Mode().Perm() != 0o700 {
			t.Fatalf("modes file=%v dir=%v", file.Mode().Perm(), dir.Mode().Perm())
		}
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("temporary file left behind: %v", entries)
	}
}

func TestApplyKeepsCommentsAndOtherLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preferences.toml")
	original := "# my tastes\nverbosity = \"normal\"   # normal | verbose | debug\n\n  color=\"never\"\nbuild_logs = true # stream them\nupdate_check = false"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	changes := []Change{{"verbosity", "debug"}, {"color", "always"}, {"build_logs", "auto"}, {"hints", "false"}}
	if err := Apply(path, changes); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	want := "# my tastes\nverbosity = \"debug\"   # normal | verbose | debug\n\n  color= \"always\"\nupdate_check = false\nhints = false\n"
	if string(data) != want {
		t.Fatalf("content =\n%q\nwant\n%q", data, want)
	}
	// A # inside a string is not a comment.
	if got := replaceValue(`color = "a#b" # note`+"\n", 6, `"never"`); got != `color = "never" # note`+"\n" {
		t.Fatalf("replaceValue = %q", got)
	}
}

func TestApplyRefusesBadInputAndBrokenFilesWithoutWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preferences.toml")
	var keyErr *KeyError
	if err := Apply(path, []Change{{"colour", "never"}}); !errors.As(err, &keyErr) {
		t.Fatalf("unknown key: %v", err)
	}
	var valueErr *ValueError
	if err := Apply(path, []Change{{"hints", "no"}}); !errors.As(err, &valueErr) || valueErr.Key != "hints" {
		t.Fatalf("bad value: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refused change created the file: %v", err)
	}
	broken := "verbosty = \"debug\"\n"
	if err := os.WriteFile(path, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	var fileErr *Error
	if err := Apply(path, []Change{{"hints", "false"}}); !errors.As(err, &fileErr) {
		t.Fatalf("broken file: %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != broken {
		t.Fatalf("broken file was rewritten: %q", data)
	}
}
