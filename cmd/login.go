package cmd

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
	"github.com/spf13/cobra"
)

func newLoginCommand(app Application, options *commandOptions, streams ui.Streams) *cobra.Command {
	var login service.LoginOptions
	var tokenStdin bool
	command := &cobra.Command{
		Use:   "login",
		Short: "Save a Coolify instance and API token for this machine",
		Long: `Save a Coolify instance URL and API token in the Coolify CLI configuration,
which coolify-cli shares, after verifying them against the server.

Interactively, login asks for the URL, a context name, and the token, which
is not echoed. Noninteractively, pass --url and --name (or --context) and
pipe the token on stdin with --token-stdin; the token is never accepted as a
flag, so it stays out of shell history and process listings.

Create a token in Coolify under Keys & Tokens with read, write, and deploy;
build logs and secret values also need sensitive read.`,
		Args: noArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			ctx := command.Context()
			prompter := ui.NewPrompter(streams)
			login.ConfigPath = options.CoolifyConfig
			if login.URL == "" && !streams.Interactive {
				return inputError(errors.New("pass --url URL (and --name NAME) when input is noninteractive"))
			}
			if login.URL != "" {
				// A bare host is refused here, before the token is asked for.
				address, err := service.NormalizeInstanceURL(login.URL)
				if err != nil {
					return inputError(fmt.Errorf("--url: %w", err))
				}
				login.URL = address
			}
			for login.URL == "" {
				value, err := prompter.Ask(ctx, "Coolify URL", "")
				if err != nil {
					return inputError(err)
				}
				address, err := service.NormalizeInstanceURL(value)
				if err != nil {
					if _, err := fmt.Fprintln(streams.Err, "Enter the instance's full URL, such as https://coolify.example.com"); err != nil {
						return err
					}
					continue
				}
				login.URL = address
			}
			if login.Name == "" {
				// The global --context names the instance for every other
				// command; here it names the one being saved.
				login.Name = options.Context
			}
			if login.Name == "" && !streams.Interactive {
				return inputError(errors.New("pass --name NAME (or --context NAME) when input is noninteractive"))
			}
			if login.Name == "" {
				value, err := prompter.Ask(ctx, "Context name", suggestName(login.URL))
				if err != nil {
					return inputError(err)
				}
				login.Name = value
			}
			switch {
			case tokenStdin:
				token, err := ui.ReadSecretLine(streams.In)
				if err != nil {
					return inputError(err)
				}
				login.Token = token
			case streams.Interactive:
				if _, err := fmt.Fprintln(streams.Err, "Create a token in Coolify under Keys & Tokens with read, write, and deploy; add sensitive read to see build logs and secret values."); err != nil {
					return err
				}
				token, err := prompter.AskSecret(ctx, "API token")
				if err != nil {
					return err
				}
				login.Token = token
			default:
				return inputError(errors.New("pass the token on stdin with --token-stdin when input is noninteractive"))
			}
			result, err := app.Login(ctx, login)
			if err != nil {
				return err
			}
			return ui.NewRenderer(streams, options.format).Login(result)
		},
	}
	command.Flags().StringVar(&login.URL, "url", "", "Coolify instance URL, e.g. https://coolify.example.com")
	command.Flags().StringVar(&login.Name, "name", "", "Context name (default: --context, or derived from the URL's host)")
	command.Flags().BoolVar(&login.Default, "default", false, "Make this the default context")
	command.Flags().BoolVar(&tokenStdin, "token-stdin", false, "Read the API token from stdin")
	return command
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

// suggestName proposes the first label of the host, so coolify.example.com
// becomes "coolify"; the user can accept or type another.
func suggestName(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	host := parsed.Hostname()
	if label, _, found := strings.Cut(host, "."); found && label != "" {
		return label
	}
	return host
}
