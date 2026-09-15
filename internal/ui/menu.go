package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"golang.org/x/term"

	"github.com/joaomnuno/coolship/internal/service"
)

// MenuGroup is one help group as the menu lists it: its title and its verbs,
// in the order the command tree declared them.
type MenuGroup struct {
	Title string
	Verbs []MenuVerb
}

// MenuVerb is one runnable command path, such as ["env", "push"], with the
// help's one-line description and the values the menu must ask for before
// the command can run.
type MenuVerb struct {
	Path   []string
	Short  string
	Inputs []MenuInput
}

// Name is the command path as typed after coolship.
func (v MenuVerb) Name() string { return strings.Join(v.Path, " ") }

// MenuInput is one value a verb cannot run without. Flag names the flag the
// value is passed as (without dashes); empty means a positional argument.
// Fields splits the answer on spaces into several positional arguments.
type MenuInput struct {
	Title       string
	Description string
	Placeholder string
	Flag        string
	Fields      bool
	Validate    func(string) error
}

// MenuOptions is what the menu needs from the command tree. Status reads the
// header; NotLinked tells the error of a directory without a binding apart
// from a failure. Refresh is how often the header is read again; zero
// leaves it to the r key. Now is the clock relative times are measured on.
type MenuOptions struct {
	Groups    []MenuGroup
	Status    func(context.Context) (service.StatusResult, error)
	NotLinked func(error) bool
	Refresh   time.Duration
	Now       func() time.Time
}

// ErrMenuNeedsTerminal is why the menu refuses to open without a terminal on
// both stdin and stdout.
var ErrMenuNeedsTerminal = errors.New("coolship ui needs a terminal on stdin and stdout; run 'coolship help' to list the commands")

// RunMenu opens the menu full-screen on the terminal and returns the
// arguments of the verb chosen there, as they would be typed after
// coolship, or nil when the menu was left without choosing. The terminal is
// restored before it returns, so the caller can run the verb and let its
// output show. A verb with inputs asks for them inline once the menu has
// closed; Esc there goes back to the menu and Ctrl-C leaves.
//
// Positional answers follow a "--" so a value that starts with a dash stays
// a value; a caller adding flags must add them before it.
//
// While the menu is open the trace is held at normal verbosity: request
// lines on stderr would draw over the screen. The level in force before is
// restored on return.
func RunMenu(ctx context.Context, streams Streams, options MenuOptions) ([]string, error) {
	streams = streams.Normalized()
	in, out, ok := menuTerminal(streams)
	if !ok {
		return nil, &service.InputError{Err: ErrMenuNeedsTerminal}
	}
	if level := streams.Trace.Level(); level != VerbosityNormal {
		streams.Trace.SetLevel(VerbosityNormal)
		defer streams.Trace.SetLevel(level)
	}
	style := streams.outPalette()
	model := newMenuModel(ctx, style, options)
	for {
		program := tea.NewProgram(model,
			tea.WithContext(ctx),
			tea.WithInput(in),
			tea.WithOutput(out),
			tea.WithoutSignalHandler(),
			tea.WithColorProfile(style.colorProfile()))
		final, err := program.Run()
		if finished, ok := final.(menuModel); ok {
			model = finished
		}
		// Whatever ended the program, no header read outlives it.
		model.stopReading()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if err != nil {
			return nil, fmt.Errorf("menu: %w", err)
		}
		if model.chosen == nil {
			return nil, nil
		}
		var interrupted atomic.Bool
		args, err := askInputs(ctx, *model.chosen, in, out, style, &interrupted)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		reopen, err := inputOutcome(err, interrupted.Load())
		if reopen {
			model = model.reopen()
			continue
		}
		if err != nil || args == nil {
			return nil, err
		}
		return args, nil
	}
}

// inputOutcome decides what an input prompt's end means: Esc goes back to
// the menu, Ctrl-C leaves without running anything, and any other error is
// returned.
func inputOutcome(err error, interrupted bool) (reopen bool, _ error) {
	switch {
	case err == nil:
		return false, nil
	case errors.Is(err, huh.ErrUserAborted) && interrupted:
		return false, nil
	case errors.Is(err, huh.ErrUserAborted):
		return true, nil
	}
	return false, err
}

// menuTerminal returns stdin and stdout when both are terminals.
func menuTerminal(streams Streams) (*os.File, *os.File, bool) {
	in, ok := streams.In.(*os.File)
	if !ok || !term.IsTerminal(int(in.Fd())) {
		return nil, nil, false
	}
	out, ok := streams.Out.(*os.File)
	if !ok || !term.IsTerminal(int(out.Fd())) {
		return nil, nil, false
	}
	return in, out, true
}

// askInputs asks for a verb's inputs, if it has any, and returns the verb's
// arguments. interrupted is set when the prompt was left with Ctrl-C rather
// than Esc; both abort the form the same way, so the key is noted as it
// passes.
func askInputs(ctx context.Context, verb MenuVerb, in, out *os.File, style palette, interrupted *atomic.Bool) ([]string, error) {
	if len(verb.Inputs) == 0 {
		return verbArgs(verb, nil), nil
	}
	form, answers := newInputForm(verb, style)
	form.WithInput(in).
		WithOutput(out).
		WithProgramOptions(tea.WithoutSignalHandler(), tea.WithColorProfile(style.colorProfile()),
			tea.WithFilter(interruptFilter(interrupted)))
	if err := form.RunWithContext(ctx); err != nil {
		return nil, err
	}
	values := make([]string, len(answers))
	for i, answer := range answers {
		values[i] = *answer
	}
	return verbArgs(verb, values), nil
}

// interruptFilter notes a Ctrl-C on its way to the form and lets every
// message through unchanged.
func interruptFilter(interrupted *atomic.Bool) func(tea.Model, tea.Msg) tea.Msg {
	return func(_ tea.Model, msg tea.Msg) tea.Msg {
		if key, ok := msg.(tea.KeyPressMsg); ok && key.String() == "ctrl+c" {
			interrupted.Store(true)
		}
		return msg
	}
}

// newInputForm builds the Huh form behind askInputs, one input field per
// value, apart from its streams so tests can inspect it.
func newInputForm(verb MenuVerb, style palette) (*huh.Form, []*string) {
	answers := make([]*string, len(verb.Inputs))
	fields := make([]huh.Field, len(verb.Inputs))
	for i, input := range verb.Inputs {
		answers[i] = new(string)
		field := huh.NewInput().
			Title("coolship " + verb.Name() + ": " + input.Title).
			Placeholder(input.Placeholder).
			Value(answers[i])
		if input.Description != "" {
			field.Description(input.Description)
		}
		if input.Validate != nil {
			field.Validate(input.Validate)
		}
		fields[i] = field
	}
	keymap := huh.NewDefaultKeyMap()
	keymap.Quit = key.NewBinding(key.WithKeys("esc", "ctrl+c"), key.WithHelp("esc", "back to the menu"))
	form := huh.NewForm(huh.NewGroup(fields...)).
		WithTheme(pickerTheme(style)).
		WithKeyMap(keymap).
		WithShowHelp(true)
	return form, answers
}

// verbArgs lays a verb's answers out as typed arguments: the path, flag
// answers as --flag=value, then "--" and the positional answers, so a
// positional value that starts with a dash is never read as a flag. The
// "--" is left out when there is no positional answer.
func verbArgs(verb MenuVerb, answers []string) []string {
	args := append([]string(nil), verb.Path...)
	var positional []string
	for i, input := range verb.Inputs {
		if i >= len(answers) {
			break
		}
		answer := strings.TrimSpace(answers[i])
		switch {
		case input.Flag != "":
			args = append(args, "--"+input.Flag+"="+answer)
		case input.Fields:
			positional = append(positional, strings.Fields(answer)...)
		default:
			positional = append(positional, answer)
		}
	}
	if len(positional) == 0 {
		return args
	}
	return append(append(args, "--"), positional...)
}

// RequireText is a MenuInput validator for a value that cannot be blank.
func RequireText(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("a value is required")
	}
	return nil
}

// RequirePositiveNumber is a MenuInput validator for a number above zero,
// such as a pull request.
func RequirePositiveNumber(value string) error {
	number, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || number <= 0 {
		return errors.New("enter a number greater than zero")
	}
	return nil
}
