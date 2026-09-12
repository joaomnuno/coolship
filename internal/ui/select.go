package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"golang.org/x/term"

	"github.com/joaomnuno/coolship/internal/service"
)

// pickerChrome is the number of lines a picker draws besides its options:
// the title, the help line, and the blank line above the help.
const pickerChrome = 3

// choiceLabels names each choice the way a selector shows it. Normal
// verbosity shows names only; a name that two choices share carries the
// detail that tells them apart, or the ID when the detail does not.
func choiceLabels(choices []service.Choice) []string {
	labels := make([]string, len(choices))
	for i, choice := range choices {
		labels[i] = singleLine(choiceName(choice))
	}
	for pass, disambiguator := range []func(service.Choice) string{
		func(c service.Choice) string { return c.Detail },
		func(c service.Choice) string { return c.ID },
	} {
		counts := map[string]int{}
		for _, label := range labels {
			counts[label]++
		}
		for i, choice := range choices {
			extra := singleLine(disambiguator(choice))
			if counts[labels[i]] < 2 || extra == "" || extra == singleLine(choiceName(choice)) {
				continue
			}
			// The second pass only reaches choices the detail did not
			// separate, and must not repeat a detail that equals the ID.
			if pass == 1 && extra == singleLine(choice.Detail) {
				continue
			}
			labels[i] += " (" + extra + ")"
		}
	}
	return labels
}

func choiceName(choice service.Choice) string {
	if choice.Name == "" {
		return choice.ID
	}
	return choice.Name
}

// terminalInput returns stdin and stderr when a picker can run on them: both
// are terminals and stderr reports a size. Anything less falls back to the
// numbered list, which reads a line at a time.
func terminalInput(streams Streams) (*os.File, *os.File, bool) {
	in, ok := streams.In.(*os.File)
	if !ok || !term.IsTerminal(int(in.Fd())) {
		return nil, nil, false
	}
	errTerminal, ok := drawable(streams)
	if !ok {
		return nil, nil, false
	}
	return in, errTerminal, true
}

// pick runs the arrow-key selector on the terminal and returns the chosen ID.
// Esc and Ctrl-C cancel; the picker is erased when it ends, and a chosen value
// leaves one "Kind: name" line behind as the record of the answer.
func (p *Prompter) pick(ctx context.Context, kind string, choices []service.Choice, in, errTerminal *os.File) (string, error) {
	height := 0
	if _, rows, err := term.GetSize(int(errTerminal.Fd())); err == nil {
		height = rows
	}
	form, value := newPicker(kind, choices, height, p.style)
	form.WithInput(in).
		WithOutput(errTerminal).
		WithProgramOptions(tea.WithoutSignalHandler(), tea.WithColorProfile(p.style.colorProfile()))
	err := form.RunWithContext(ctx)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return "", ctxErr
	}
	switch {
	case errors.Is(err, huh.ErrUserAborted):
		return "", service.ErrCancelled
	case err != nil:
		return "", fmt.Errorf("select %s: %w", singleLine(kind), err)
	}
	labels := choiceLabels(choices)
	for i, choice := range choices {
		if choice.ID == *value {
			if _, err := fmt.Fprintln(p.streams.Err, p.style.key(capitalize(singleLine(kind)))+" "+labels[i]); err != nil {
				return "", err
			}
			return choice.ID, nil
		}
	}
	return "", service.ErrCancelled
}

// newPicker builds the Huh select behind pick, apart from its streams, so
// tests can drive it with key messages. The cursor starts on the current
// choice. When the options would not fit in height terminal rows, typing
// filters them straight away; otherwise "/" starts a filter. The form's
// quit keys are Esc and Ctrl-C, checked before the field sees a key.
func newPicker(kind string, choices []service.Choice, height int, style palette) (*huh.Form, *string) {
	value := new(string)
	labels := choiceLabels(choices)
	options := make([]huh.Option[string], len(choices))
	for i, choice := range choices {
		options[i] = huh.NewOption(labels[i], choice.ID).Selected(choice.Current)
	}
	title := "Select " + singleLine(kind)
	field := huh.NewSelect[string]().Title(title).Options(options...).Value(value)
	if height > 0 && len(choices)+pickerChrome > height {
		// While filtering, the filter input takes the title's place.
		field.Filtering(true).Description(title + "; type to filter")
	}
	keymap := huh.NewDefaultKeyMap()
	keymap.Quit = key.NewBinding(key.WithKeys("esc", "ctrl+c"), key.WithHelp("esc", "cancel"))
	form := huh.NewForm(huh.NewGroup(field)).
		WithTheme(pickerTheme(style)).
		WithKeyMap(keymap).
		WithShowHelp(true)
	return form, value
}

// pickerTheme matches the prompts around the picker: a bold question and a
// cyan cursor on the chosen line, on Huh's plain base theme. The program's
// colour profile decides whether any of it reaches the terminal.
func pickerTheme(style palette) huh.Theme {
	return huh.ThemeFunc(func(isDark bool) *huh.Styles {
		theme := huh.ThemeBase(isDark)
		theme.Focused.Title = theme.Focused.Title.Bold(true)
		theme.Focused.SelectSelector = theme.Focused.SelectSelector.Foreground(lipgloss.Cyan)
		theme.Focused.SelectedOption = theme.Focused.SelectedOption.Foreground(lipgloss.Cyan)
		theme.Focused.Description = style.styles[dim]
		return theme
	})
}

func capitalize(text string) string {
	if text == "" {
		return text
	}
	return strings.ToUpper(text[:1]) + text[1:]
}
