package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"golang.org/x/term"

	"github.com/joaomnuno/coolship/internal/preferences"
	"github.com/joaomnuno/coolship/internal/service"
)

// PreferencesEditable reports whether `coolship config` opens the form: human
// output, interactive input, and stdin, stdout, and stderr all terminals.
// Anything less prints the configuration exactly as `config show` does, so a
// pipe or a script never meets a form.
func PreferencesEditable(streams Streams, format string) bool {
	if format != "human" || !streams.Interactive || !streams.OutTerminal {
		return false
	}
	in, ok := streams.In.(*os.File)
	if !ok || !term.IsTerminal(int(in.Fd())) {
		return false
	}
	errTerminal, ok := streams.Err.(*os.File)
	return ok && term.IsTerminal(int(errTerminal.Fd()))
}

// PreferencesForm is what the config form shows above the fields: the
// configuration as `config show` reports it, or the reason there is none
// for this directory, and the preferences file.
type PreferencesForm struct {
	Config      service.ConfigResult
	ConfigError error
	Report      preferences.Report
}

// EditPreferences runs the config form on the terminal and returns the
// changes to write: only keys whose value differs from the one in effect
// when the form opened. Save returns them; Discard returns none; Esc and
// Ctrl-C cancel with service.ErrCancelled. The form writes nothing itself.
func EditPreferences(ctx context.Context, streams Streams, input PreferencesForm) ([]preferences.Change, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	in, inOK := streams.In.(*os.File)
	errTerminal, errOK := streams.Err.(*os.File)
	if !inOK || !errOK {
		return nil, errors.New("the config form needs a terminal; use coolship config set KEY VALUE")
	}
	style := streams.errPalette()
	model := newPreferencesForm(input, style)
	model.form.WithInput(in).
		WithOutput(errTerminal).
		WithProgramOptions(tea.WithoutSignalHandler(), tea.WithColorProfile(style.colorProfile()))
	err := model.form.RunWithContext(ctx)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	switch {
	case errors.Is(err, huh.ErrUserAborted):
		return nil, service.ErrCancelled
	case err != nil:
		return nil, fmt.Errorf("config form: %w", err)
	}
	return model.changes(), nil
}

// preferencesForm is the Huh form behind EditPreferences, apart from its
// streams, so tests can drive it with key messages.
type preferencesForm struct {
	form    *huh.Form
	initial map[string]string
	values  map[string]*string
	save    *bool
}

func newPreferencesForm(input PreferencesForm, style palette) *preferencesForm {
	model := &preferencesForm{initial: map[string]string{}, values: map[string]*string{}, save: new(bool)}
	*model.save = true
	fields := []huh.Field{}
	for _, descriptor := range preferences.Keys() {
		current, _, _ := input.Report.Preferences.Get(descriptor.Name)
		value := new(string)
		*value = current
		model.initial[descriptor.Name] = current
		model.values[descriptor.Name] = value
		options := make([]huh.Option[string], len(descriptor.Allowed))
		for i, allowed := range descriptor.Allowed {
			options[i] = huh.NewOption(allowed, allowed).Selected(allowed == current)
		}
		fields = append(fields, huh.NewSelect[string]().
			Key(descriptor.Name).
			Title(descriptor.Name).
			Description(descriptor.Description).
			Options(options...).
			Inline(true).
			Value(value))
	}
	fields = append(fields, huh.NewConfirm().
		Title("Save to "+singleLine(input.Report.Path)+"?").
		Affirmative("Save").
		Negative("Discard").
		Value(model.save))
	keymap := huh.NewDefaultKeyMap()
	keymap.Quit = key.NewBinding(key.WithKeys("esc", "ctrl+c"), key.WithHelp("esc", "cancel"))
	// Each list is a handful of words; filtering one would only surprise.
	keymap.Select.Filter = key.NewBinding(key.WithDisabled())
	group := huh.NewGroup(fields...).
		Title("Coolship preferences").
		Description(preferencesHeader(input, style))
	model.form = huh.NewForm(group).
		WithTheme(pickerTheme(style)).
		WithKeyMap(keymap).
		WithShowHelp(true)
	return model
}

// changes lists the keys the form changed, in the order the form shows
// them, or nothing when the form was discarded.
func (m *preferencesForm) changes() []preferences.Change {
	if !*m.save {
		return nil
	}
	var changes []preferences.Change
	for _, descriptor := range preferences.Keys() {
		if value := *m.values[descriptor.Name]; value != m.initial[descriptor.Name] {
			changes = append(changes, preferences.Change{Key: descriptor.Name, Value: value})
		}
	}
	return changes
}

// preferencesHeader is the read-only part of the form: the binding, where
// credentials come from, the instance, and the preferences file. A token is
// never part of a ConfigResult, so it cannot appear. Without a binding the
// credentials and instance are still shown, from what Config could read.
func preferencesHeader(input PreferencesForm, style palette) string {
	result := input.Config
	binding := ""
	if input.ConfigError != nil {
		binding = "none (" + input.ConfigError.Error() + ")"
	} else {
		b := result.Binding
		binding = b.Project + " / " + b.Environment + " / " + b.Application
		if result.Target != "" && result.Target != "default" {
			binding += " [" + result.Target + "]"
		}
	}
	rows := [][2]string{{"Binding", binding},
		{"Credentials", describeCredentials(result)}, {"Instance", describeInstance(result)}}
	file := input.Report.Path
	switch {
	case input.Report.Err != nil && file == "":
		file = "(not found: " + input.Report.Err.Error() + ")"
	case input.Report.Err != nil:
		var typed *preferences.Error
		reason := input.Report.Err.Error()
		if errors.As(input.Report.Err, &typed) {
			reason = typed.Err.Error()
		}
		file += " (ignored: " + reason + ")"
	case !input.Report.Present:
		file += " (absent; saving creates it)"
	}
	rows = append(rows, [2]string{"Preferences", file})
	var lines []string
	for _, row := range rows {
		if row[1] == "" {
			continue
		}
		padding := strings.Repeat(" ", max(0, 13-utf8.RuneCountInString(row[0]+":")))
		lines = append(lines, style.key(row[0])+padding+" "+singleLine(row[1]))
	}
	return strings.Join(lines, "\n")
}

// PreferenceResult is what config get and config set report: the key, the
// value in effect, whether the file sets it, and the file.
type PreferenceResult struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Set   bool   `json:"set"`
	Path  string `json:"path,omitempty"`
}

// PreferenceGet prints the value alone, like `gh config get`, so a script
// can read it with command substitution; JSON adds whether the file sets it.
func (r *Renderer) PreferenceGet(result PreferenceResult) error {
	if r.format == "json" {
		return json.NewEncoder(r.streams.Out).Encode(result)
	}
	_, err := fmt.Fprintln(r.streams.Out, singleLine(result.Value))
	return err
}

// PreferencesSaved reports a config set or a saved form. Like `gh config
// set`, human output prints nothing on stdout, so a script's output stays
// its own; a terminal gets one confirmation line on stderr.
func (r *Renderer) PreferencesSaved(path string, changes []preferences.Change) error {
	if r.format == "json" {
		results := make([]PreferenceResult, len(changes))
		for i, change := range changes {
			results[i] = PreferenceResult{Key: change.Key, Value: change.Value, Set: !isAutoChange(change), Path: path}
		}
		if len(results) == 1 {
			return json.NewEncoder(r.streams.Out).Encode(results[0])
		}
		return json.NewEncoder(r.streams.Out).Encode(results)
	}
	if !r.streams.ErrTerminal {
		return nil
	}
	if len(changes) == 0 {
		_, err := fmt.Fprintln(r.streams.Err, r.err.apply(dim, "No preferences changed"))
		return err
	}
	set := make([]string, len(changes))
	for i, change := range changes {
		set[i] = change.Key + " = " + change.Value
	}
	_, err := fmt.Fprintf(r.streams.Err, "%s Saved %s to %s\n", r.err.apply(green, "✓"), singleLine(strings.Join(set, ", ")), singleLine(path))
	return err
}

func isAutoChange(change preferences.Change) bool {
	descriptor, _ := preferences.Lookup(change.Key)
	return descriptor.Kind == preferences.KindOptionalBool && change.Value == preferences.Auto
}
