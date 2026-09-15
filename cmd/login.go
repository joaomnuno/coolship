package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
	"github.com/spf13/cobra"
)

func newLoginCommand(app Application, options *commandOptions, streams ui.Streams, openBrowser func(string) error) *cobra.Command {
	var login service.LoginOptions
	var tokenStdin bool
	command := &cobra.Command{
		Use:   "login",
		Short: "Save a Coolify instance and API token for this machine",
		Long: `Save a Coolify instance URL and API token in the Coolify CLI configuration,
which coolify-cli shares, after verifying them against the server.

In a terminal, login asks step by step: self-hosted or Coolify Cloud, the
URL, a context name, and the token, which is not echoed. The URL is checked
as soon as it is entered, through Coolify's public health check, and the
token as soon as it is typed. A failed check explains what happened and
offers to retry, go back, or leave. Shift+Tab edits the previous answer; Esc
leaves without writing anything.

Noninteractively, pass --url and --name (or --context) and pipe the token on
stdin with --token-stdin; the same checks run, and a failure exits with its
error code. For Coolify Cloud, pass --url https://app.coolify.io. The token
is never accepted as a flag, so it stays out of shell history and process
listings.

A URL and token that a saved context already holds are refused. Create a
token in Coolify under Keys & Tokens with read, write, and deploy; build logs
and secret values also need sensitive read.`,
		Args: noArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			ctx := command.Context()
			login.ConfigPath = options.CoolifyConfig
			if login.URL != "" {
				// A bare host is refused here, before the token is asked for.
				address, err := service.NormalizeInstanceURL(login.URL)
				if err != nil {
					return inputError(fmt.Errorf("--url: %w", err))
				}
				login.URL = address
			}
			if login.Name == "" {
				// The global --context names the instance for every other
				// command; here it names the one being saved.
				login.Name = options.Context
			}
			renderer := ui.NewRenderer(streams, options.format)
			// With a terminal on stdin, --token-stdin would read the token
			// from that same terminal, so the form asks for it instead,
			// unechoed, with every step's Retry, Go back, and Leave.
			if ui.LoginFormAvailable(streams, options.format) {
				result, err := ui.RunLoginForm(ctx, streams, options.format, app, login, openBrowser)
				if err != nil {
					return err
				}
				return renderer.Login(result)
			}
			if !streams.Interactive {
				return loginWithoutPrompts(ctx, app, login, tokenStdin, streams, renderer)
			}
			result, err := loginLineByLine(ctx, app, login, tokenStdin, streams)
			if err != nil {
				return err
			}
			return renderer.Login(result)
		},
	}
	command.Flags().StringVar(&login.URL, "url", "", "Coolify instance URL, e.g. https://coolify.example.com or https://app.coolify.io")
	command.Flags().StringVar(&login.Name, "name", "", "Context name (default: --context, or derived from the URL's host)")
	command.Flags().BoolVar(&login.Default, "default", false, "Make this the default context")
	command.Flags().BoolVar(&tokenStdin, "token-stdin", false, "Read the API token from stdin")
	return command
}

// loginWithoutPrompts takes everything from flags and stdin, refusing by flag
// name what is missing before stdin is read, then runs every check.
func loginWithoutPrompts(ctx context.Context, app Application, login service.LoginOptions, tokenStdin bool, streams ui.Streams, renderer *ui.Renderer) error {
	switch {
	case login.URL == "":
		return inputError(errors.New("pass --url URL (and --name NAME) when input is noninteractive"))
	case login.Name == "":
		return inputError(errors.New("pass --name NAME (or --context NAME) when input is noninteractive"))
	case !tokenStdin:
		return inputError(errors.New("pass the token on stdin with --token-stdin when input is noninteractive"))
	}
	token, err := ui.ReadSecretLine(streams.In)
	if err != nil {
		return inputError(err)
	}
	login.Token = token
	result, err := app.Login(ctx, login)
	if err != nil {
		return err
	}
	return renderer.Login(result)
}

// loginLineByLine asks one line at a time, for input that is interactive but
// cannot run the form: a run at --verbose or --debug, or interactive input
// that is not a terminal. The checks run in the
// form's order: the instance right after its URL, then the token.
func loginLineByLine(ctx context.Context, app Application, login service.LoginOptions, tokenStdin bool, streams ui.Streams) (service.LoginResult, error) {
	prompter := ui.NewPrompter(streams)
	for login.URL == "" {
		value, err := prompter.Ask(ctx, "Coolify URL (https://app.coolify.io for Coolify Cloud)", "")
		if err != nil {
			return service.LoginResult{}, inputError(err)
		}
		address, err := service.NormalizeInstanceURL(value)
		if err != nil {
			if _, err := fmt.Fprintln(streams.Err, "Enter the instance's full URL, such as https://coolify.example.com"); err != nil {
				return service.LoginResult{}, err
			}
			continue
		}
		login.URL = address
	}
	address, err := app.CheckInstance(ctx, login.URL)
	if err != nil {
		return service.LoginResult{}, err
	}
	login.URL = address
	if login.Name == "" {
		value, err := prompter.Ask(ctx, "Context name", service.SuggestContextName(login.URL, app.SavedContexts(login.ConfigPath)))
		if err != nil {
			return service.LoginResult{}, inputError(err)
		}
		login.Name = value
	}
	if tokenStdin {
		token, err := ui.ReadSecretLine(streams.In)
		if err != nil {
			return service.LoginResult{}, inputError(err)
		}
		login.Token = token
	} else {
		if _, err := fmt.Fprintf(streams.Err, "Create a token in Coolify under Keys & Tokens: %s/security/api-tokens\n", login.URL); err != nil {
			return service.LoginResult{}, err
		}
		token, err := prompter.AskSecret(ctx, "API token")
		if err != nil {
			return service.LoginResult{}, err
		}
		login.Token = token
	}
	check, err := app.CheckLogin(ctx, login)
	if err != nil {
		return service.LoginResult{}, err
	}
	return app.SaveLogin(ctx, login, check)
}

func newLogoutCommand(app Application, options *commandOptions, streams ui.Streams) *cobra.Command {
	return &cobra.Command{
		Use:   "logout NAME",
		Short: "Remove a saved Coolify instance from this machine",
		Long: `Remove a context from the Coolify CLI configuration. The token remains valid
on the server until you revoke it in Coolify.`,
		Args: func(command *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(command, args); err != nil {
				return inputError(err)
			}
			return nil
		},
		RunE: func(command *cobra.Command, args []string) error {
			result, err := app.Logout(command.Context(), service.LogoutOptions{ConfigPath: options.CoolifyConfig, Name: args[0]})
			if err != nil {
				return err
			}
			return ui.NewRenderer(streams, options.format).Logout(result)
		},
	}
}
