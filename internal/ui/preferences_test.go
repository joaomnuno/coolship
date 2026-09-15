package ui

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/joaomnuno/coolship/internal/config"
	"github.com/joaomnuno/coolship/internal/preferences"
	"github.com/joaomnuno/coolship/internal/service"
)

var keyRight = tea.KeyPressMsg{Code: tea.KeyRight}

func startPreferencesForm(t *testing.T, input PreferencesForm) *preferencesForm {
	t.Helper()
	model := newPreferencesForm(input, newPalette(false))
	drive(t, model.form, nil)
	drive(t, model.form, tea.WindowSizeMsg{Width: 90, Height: 40})
	return model
}

var formInput = PreferencesForm{
	Config: service.ConfigResult{Target: "default", CredentialSource: "file", CredentialPath: "/home/u/.config/coolify/config.json",
		Instance: "home", InstanceURL: "https://coolify.example.com",
		Binding: config.Binding{Context: "home", Project: "Personal", Environment: "production", Application: "fenix-bot"}},
	Report: preferences.Report{Path: "/home/u/.config/coolship/preferences.toml", Present: true,
		Preferences: preferences.Preferences{Verbosity: preferences.VerbosityVerbose}},
}

func TestPreferencesFormShowsHeaderAndCurrentValues(t *testing.T) {
	model := startPreferencesForm(t, formInput)
	view := model.form.View()
	for _, want := range []string{"Coolship preferences", "Binding:", "Personal / production / fenix-bot", "Credentials:", "/home/u/.config/coolify/config.json",
		"Instance:", "home at https://coolify.example.com", "Preferences:", "verbosity", "build_logs", "color", "hints", "update_check"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
	for name, want := range map[string]string{"verbosity": "verbose", "build_logs": "auto", "color": "auto", "hints": "true", "update_check": "true"} {
		if got := *model.values[name]; got != want {
			t.Errorf("%s starts at %q; want %q", name, got, want)
		}
	}
	// Without a binding the header says why, still shows the credentials and
	// instance Config could read, and still edits preferences.
	input := formInput
	input.ConfigError = errors.New("not linked")
	input.Config = service.ConfigResult{CredentialSource: "file", CredentialPath: "/home/u/.config/coolify/config.json",
		Instance: "home", InstanceURL: "https://coolify.example.com"}
	input.Report = preferences.Report{Path: "/p.toml"}
	view = startPreferencesForm(t, input).form.View()
	if !strings.Contains(view, "Binding:      none (not linked)") || !strings.Contains(view, "Credentials:  /home/u/.config/coolify/config.json") ||
		!strings.Contains(view, "Instance:     home at https://coolify.example.com") || !strings.Contains(view, "/p.toml (absent; saving creates it)") {
		t.Fatalf("unlinked header:\n%s", view)
	}
}

func TestPreferencesFormSavesOnlyChangedKeys(t *testing.T) {
	model := startPreferencesForm(t, formInput)
	// verbosity: verbose -> debug; build_logs, color: unchanged; hints: true -> false.
	drive(t, model.form, keyRight, keyEnter, keyEnter, keyEnter, keyRight, keyEnter, keyEnter, keyEnter)
	if model.form.State != huh.StateCompleted {
		t.Fatalf("state = %v; want completed\n%s", model.form.State, model.form.View())
	}
	want := []preferences.Change{{Key: "verbosity", Value: "debug"}, {Key: "hints", Value: "false"}}
	if got := model.changes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("changes = %+v; want %+v", got, want)
	}
}

func TestPreferencesFormDiscardAndEsc(t *testing.T) {
	model := startPreferencesForm(t, formInput)
	drive(t, model.form, keyRight, keyEnter, keyEnter, keyEnter, keyEnter, keyEnter, keyRight, keyEnter)
	if model.form.State != huh.StateCompleted || model.changes() != nil {
		t.Fatalf("discard: state=%v changes=%+v", model.form.State, model.changes())
	}
	model = startPreferencesForm(t, formInput)
	drive(t, model.form, keyRight, keyEsc)
	if model.form.State != huh.StateAborted {
		t.Fatalf("esc: state = %v; want aborted", model.form.State)
	}
}

func TestPreferencesEditableNeedsTerminalsAndHumanOutput(t *testing.T) {
	var buffer bytes.Buffer
	for _, streams := range []Streams{
		{In: strings.NewReader(""), Out: &buffer, Err: &buffer, Interactive: true, OutTerminal: true},
		{},
	} {
		if PreferencesEditable(streams, "human") {
			t.Fatalf("buffers are not terminals: %+v", streams)
		}
	}
}

func TestPreferenceOutput(t *testing.T) {
	var out, diagnostic bytes.Buffer
	streams := Streams{Out: &out, Err: &diagnostic}
	if err := NewRenderer(streams, "human").PreferenceGet(PreferenceResult{Key: "hints", Value: "true"}); err != nil || out.String() != "true\n" {
		t.Fatalf("get = %q, %v", out.String(), err)
	}
	out.Reset()
	changes := []preferences.Change{{Key: "hints", Value: "false"}}
	if err := NewRenderer(streams, "human").PreferencesSaved("/p.toml", changes); err != nil || out.Len() != 0 || diagnostic.Len() != 0 {
		t.Fatalf("piped set printed out=%q err=%q", out.String(), diagnostic.String())
	}
	streams.ErrTerminal = true
	if err := NewRenderer(streams, "human").PreferencesSaved("/p.toml", changes); err != nil || diagnostic.String() != "✓ Saved hints = false to /p.toml\n" {
		t.Fatalf("terminal set stderr=%q", diagnostic.String())
	}
	if err := NewRenderer(streams, "json").PreferencesSaved("/p.toml", []preferences.Change{{Key: "build_logs", Value: "auto"}}); err != nil {
		t.Fatal(err)
	}
	var decoded PreferenceResult
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil || decoded != (PreferenceResult{Key: "build_logs", Value: "auto", Set: false, Path: "/p.toml"}) {
		t.Fatalf("json set = %s, %v", out.String(), err)
	}
}
