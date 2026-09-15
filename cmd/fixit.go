package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/joaomnuno/coolship/internal/auth"
	"github.com/joaomnuno/coolship/internal/problem"
	"github.com/joaomnuno/coolship/internal/project"
	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
	"github.com/spf13/cobra"
)

// Execute runs one command line through a new command tree. When the command
// fails in a way Coolship can set right, and a person is there to answer
// (human output, interactive streams, hints on), the failure is printed and a
// fix is offered once: log in, log in again, link or init, pick a saved
// context, or open the Coolify page where an instance admin changes the
// setting. After a fix the command runs again with the same arguments: at
// once when it only reads, after "Continue with coolship X?" when it changes
// something. Declining, or a fix that does not finish, returns the original
// failure, already printed, with its exit code. A failure of the rerun is
// returned as it is; there is never a second offer.
func Execute(ctx context.Context, app Application, streams ui.Streams, version string, args []string, opts ...Option) error {
	streams = sharedInput(streams.Normalized())
	root := NewRootCommand(app, streams, version, opts...)
	root.SetArgs(args)
	command, err := root.ExecuteContextC(ctx)
	if err == nil {
		return nil
	}
	f := &fixer{app: app, streams: streams, version: version, opts: opts, config: collectSettings(opts),
		root: root, command: command, args: args}
	var follow *followUpError
	if errors.As(err, &follow) {
		f.args, f.followUp = follow.args, follow.args[0]
	}
	return f.offer(ctx, err)
}

// followUpError is the failure of a command another one started once its own
// work was done, such as the deployment after init created and linked the
// application. A fix runs that command again, not the whole line.
type followUpError struct {
	args []string // the follow-up's command line, its name first
	err  error
}

func (e *followUpError) Error() string { return e.err.Error() }
func (e *followUpError) Unwrap() error { return e.err }

// followUp marks err, when there is one, as the failure of the command args
// name.
func followUp(err error, args []string) error {
	if err == nil {
		return nil
	}
	return &followUpError{args: args, err: err}
}

// mutatingCommands change something on the server, so a rerun after a fix
// asks first.
var mutatingCommands = []string{"deploy", "preview", "cancel", "start", "stop", "restart", "env push", "domain set"}

// noFixCommands never get an offer: login and logout are the fixes
// themselves, and the rest only inspect or change local files.
var noFixCommands = []string{"login", "logout", "unlink", "config", "alias"}

type fixer struct {
	app     Application
	streams ui.Streams
	version string
	opts    []Option
	config  settings
	root    *cobra.Command
	command *cobra.Command // the command that failed
	args    []string       // the command line a fix runs again
	// followUp names the command another one started, when that is what
	// failed; the rerun is that command alone.
	followUp string
}

// offer prints failure and puts the fix to the user, returning what the
// invocation should end with.
func (f *fixer) offer(ctx context.Context, failure error) error {
	if ctx.Err() != nil || !f.eligible() {
		return failure
	}
	found, ok := problem.Classify(failure)
	if !ok || !f.feasible(found) {
		return failure
	}
	if err := ui.ReportError(f.streams, "human", failure); err != nil {
		return failure
	}
	prompter := ui.NewPrompter(f.streams)
	args, fixed, err := f.fix(ctx, prompter, found)
	switch {
	case ctx.Err() != nil:
		return ctx.Err()
	case err != nil:
		if !errors.Is(err, service.ErrCancelled) {
			_ = ui.ReportError(f.streams, "human", err)
		}
		return ui.Reported(failure)
	case !fixed:
		return ui.Reported(failure)
	}
	name := f.name()
	// Opening a page fixes nothing by itself: the rerun waits for the person
	// who changes the setting, whatever the command does.
	if slices.Contains(mutatingCommands, name) || found.Fix == problem.FixOpenURL {
		yes, err := prompter.YesNo(ctx, "Continue with coolship "+name+"?", true)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil || !yes {
			return ui.Reported(failure)
		}
	}
	return f.execute(ctx, args)
}

// eligible reports whether anyone is there to accept a fix for this command.
func (f *fixer) eligible() bool {
	format := f.root.PersistentFlags().Lookup("format")
	if format == nil || format.Value.String() != "human" || !f.streams.Interactive || !f.config.preferences.Preferences.HintsEnabled() {
		return false
	}
	if f.command == nil || f.command == f.root || offline(f.command) {
		return false
	}
	return !slices.Contains(noFixCommands, strings.Fields(f.name())[0])
}

// feasible reports whether the fix the catalog names can be carried out here.
// Credentials from COOLSHIP_URL and COOLSHIP_TOKEN win over any login, so a
// login would change nothing; logging in again needs the saved context that
// answered; a page needs a browser launcher.
func (f *fixer) feasible(found problem.Problem) bool {
	switch found.Fix {
	case problem.FixLogin, problem.FixPickContext:
		return !f.environmentCredentials()
	case problem.FixReLogin:
		name, _ := f.savedContext(found.Instance)
		return !f.environmentCredentials() && name != ""
	case problem.FixLink:
		return true
	case problem.FixOpenURL:
		return f.config.openBrowser != nil && found.FixURL != ""
	}
	return false
}

func (f *fixer) environmentCredentials() bool {
	env := f.config.environment
	return env != nil && (env("COOLSHIP_URL") != "" || env("COOLSHIP_TOKEN") != "")
}

// fix carries out the fix and returns the arguments to run again. fixed is
// false when the user declined.
func (f *fixer) fix(ctx context.Context, prompter *ui.Prompter, found problem.Problem) ([]string, bool, error) {
	globals := globalArgs(f.root)
	switch found.Fix {
	case problem.FixLogin:
		f.say("Coolship can run coolship login here, then run coolship " + f.name() + " again.")
		if yes, err := prompter.YesNo(ctx, "Set it up now?", true); err != nil || !yes {
			return nil, false, err
		}
		return f.args, true, f.execute(ctx, append([]string{"login"}, globals...))
	case problem.FixReLogin:
		name, address := f.savedContext(found.Instance)
		f.say("Coolship can log in to " + name + " again with a new token, then run coolship " + f.name() + " again.")
		if yes, err := prompter.YesNo(ctx, "Set it up now?", true); err != nil || !yes {
			return nil, false, err
		}
		return f.args, true, f.execute(ctx, append([]string{"login", "--url=" + address, "--name=" + name}, globals...))
	case problem.FixLink:
		choice, err := prompter.Select(ctx, "how to link this directory", []service.Choice{
			{ID: "link", Name: "Link an existing application (coolship link)"},
			{ID: "init", Name: "Create a new application (coolship init)"},
			{ID: "leave", Name: "Leave"},
		})
		if err != nil || choice == "leave" {
			return nil, false, err
		}
		// The failed command runs next, so link or init must not deploy or
		// suggest deploying: deploy after init would otherwise queue twice.
		return f.args, true, f.execute(ctx, append([]string{choice}, globals...), withRerunPending())
	case problem.FixPickContext:
		return f.pickContext(ctx, prompter, found, globals)
	case problem.FixOpenURL:
		return f.openPage(ctx, prompter, found, globals)
	}
	return nil, false, nil
}

// pickContext offers the saved contexts, a login, or leaving. A saved
// context applies to the rerun only, as --context would.
func (f *fixer) pickContext(ctx context.Context, prompter *ui.Prompter, found problem.Problem, globals []string) ([]string, bool, error) {
	contexts, err := f.app.Contexts(f.serviceOptions())
	if err != nil {
		contexts = nil
	}
	const login, leave = "\x00login", "\x00leave"
	var choices []service.Choice
	for _, saved := range contexts {
		choices = append(choices, service.Choice{ID: saved.Name, Name: saved.Name, Detail: saved.URL})
	}
	loginLabel := "Log in to another instance (coolship login)"
	if found.Context != "" {
		loginLabel = "Log in and save it as " + found.Context + " (coolship login)"
	}
	choices = append(choices, service.Choice{ID: login, Name: loginLabel}, service.Choice{ID: leave, Name: "Leave"})
	choice, err := prompter.Select(ctx, "context", choices)
	switch {
	case err != nil || choice == leave:
		return nil, false, err
	case choice == login:
		args := append([]string{"login"}, globals...)
		if found.Context != "" {
			args = append(args, "--name="+found.Context)
		}
		if found.Code == problem.CodeNoDefaultContext {
			args = append(args, "--default")
		}
		return f.args, true, f.execute(ctx, args)
	}
	f.say("Using context " + choice + " for this run; pass --context " + choice + " next time.")
	return withContext(f.args, choice), true, nil
}

// openPage opens the Coolify page where the setting changes. A token that
// lacks permissions is replaced there, so login for that context follows.
func (f *fixer) openPage(ctx context.Context, prompter *ui.Prompter, found problem.Problem, globals []string) ([]string, bool, error) {
	f.say("Coolship can open " + found.FixURL + " in your browser.")
	if yes, err := prompter.YesNo(ctx, "Set it up now?", true); err != nil || !yes {
		return nil, false, err
	}
	if err := f.config.openBrowser(found.FixURL); err != nil {
		f.say("Could not open a browser; visit " + found.FixURL)
	}
	switch found.Code {
	case problem.CodeMissingPermissions, problem.CodeTokenExceedsRole:
		if name, address := f.savedContext(found.Instance); name != "" && !f.environmentCredentials() {
			f.say("Create a token with the permissions there, then enter it here.")
			return f.args, true, f.execute(ctx, append([]string{"login", "--url=" + address, "--name=" + name}, globals...))
		}
		f.say("Create a token with the permissions there, then run coolship login.")
	default:
		f.say("Change the setting in Coolify, then continue.")
	}
	return f.args, true, nil
}

// savedContext names the saved context for an instance URL. When several
// share it, the one the credentials came from wins: the context --context
// names, else the one coolship.toml commits for the target, else the default.
func (f *fixer) savedContext(instance string) (string, string) {
	instance = strings.TrimRight(instance, "/")
	if instance == "" {
		return "", ""
	}
	contexts, err := f.app.Contexts(f.serviceOptions())
	if err != nil {
		return "", ""
	}
	wanted := f.flag("context")
	if wanted == "" {
		wanted = f.committedContext()
	}
	var first, fallback, exact *auth.Instance
	for i := range contexts {
		saved := &contexts[i]
		if strings.TrimRight(saved.URL, "/") != instance {
			continue
		}
		switch {
		case wanted != "" && saved.Name == wanted:
			exact = saved
		case wanted == "" && saved.Default:
			fallback = saved
		case first == nil:
			first = saved
		}
	}
	for _, found := range []*auth.Instance{exact, fallback, first} {
		if found != nil {
			return found.Name, found.URL
		}
	}
	return "", ""
}

// committedContext is the context coolship.toml names for the selected
// target, or "" when there is none to read.
func (f *fixer) committedContext() string {
	p, err := project.Discover(project.Paths{CWD: f.flag("cwd"), ConfigPath: f.flag("config")}, false)
	if err != nil {
		return ""
	}
	target, err := project.Select(p, f.flag("target"), f.flag("environment"))
	if err != nil {
		return ""
	}
	return target.Binding.Context
}

func (f *fixer) serviceOptions() service.Options {
	return service.Options{CWD: f.flag("cwd"), ConfigPath: f.flag("config"), Context: f.flag("context"), CoolifyConfig: f.flag("coolify-config")}
}

func (f *fixer) flag(name string) string {
	if flag := f.root.PersistentFlags().Lookup(name); flag != nil {
		return flag.Value.String()
	}
	return ""
}

// name is the failed command's path below the root, such as "env push".
func (f *fixer) name() string {
	if f.followUp != "" {
		return f.followUp
	}
	return strings.TrimPrefix(f.command.CommandPath(), f.root.CommandPath()+" ")
}

func (f *fixer) say(text string) {
	_, _ = fmt.Fprintln(f.streams.Err, text)
}

// execute runs a command line in a new tree, which offers no fix of its own.
func (f *fixer) execute(ctx context.Context, args []string, extra ...Option) error {
	root := NewRootCommand(f.app, f.streams, f.version, append(slices.Clone(f.opts), extra...)...)
	root.SetArgs(args)
	return root.ExecuteContext(ctx)
}

// withContext returns args with any --context replaced by name, placed before
// a "--" so it is read as a flag.
func withContext(args []string, name string) []string {
	var result []string
	end := len(args)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			end = i
			break
		}
		switch {
		case arg == "--context":
			i++
			continue
		case strings.HasPrefix(arg, "--context="):
			continue
		}
		result = append(result, arg)
	}
	result = append(result, "--context="+name)
	return append(result, args[end:]...)
}

// collectSettings applies the options to an empty settings value.
func collectSettings(opts []Option) settings {
	var config settings
	for _, opt := range opts {
		if opt != nil {
			opt(&config)
		}
	}
	return config
}

// sharedInput gives every prompt of one invocation, follow-ups and fixes
// included, the same buffered reader, so a line one prompt reads ahead is not
// lost to the next. A terminal is left as it is: the pickers need the file,
// and a terminal delivers one line per read.
func sharedInput(streams ui.Streams) ui.Streams {
	switch streams.In.(type) {
	case *os.File, *bufio.Reader:
		return streams
	}
	streams.In = bufio.NewReader(streams.In)
	return streams
}
