package ui

import (
	"context"
	"errors"
	"strings"
	"unicode"

	"github.com/joaomnuno/coolship/internal/service"
)

// LoginActions is what the login form needs from the service: the saved
// contexts to list, and the three steps it runs between questions.
type LoginActions interface {
	SavedContexts(configPath string) []service.SavedContext
	CheckInstance(ctx context.Context, url string) (string, error)
	CheckLogin(ctx context.Context, options service.LoginOptions) (service.LoginCheck, error)
	SaveLogin(ctx context.Context, options service.LoginOptions, check service.LoginCheck) (service.LoginResult, error)
}

// Login's checklist rows, in order.
const (
	loginType = iota
	loginURL
	loginCheckInstance
	loginName
	loginToken
	loginCheckToken
	loginSave
)

var loginTitles = []string{"Instance type", "URL", "Check instance", "Context name", "Token", "Check token", "Save"}

// LoginFormAvailable reports whether login can run its form: interactive
// input from a terminal, and stderr a terminal it can draw on at normal
// verbosity. Anything less asks line by line, or not at all.
func LoginFormAvailable(streams Streams) bool {
	_, _, ok := terminalInput(streams.Normalized())
	return ok
}

// RunLoginForm asks for a login step by step on the terminal. preset carries
// what flags already said (URL, name, the credentials path, --default); those
// questions are answered with it and not asked. The instance is checked
// right after its URL, and the token right after it is typed; nothing is
// written until the last step. Leaving returns service.ErrCancelled.
func RunLoginForm(ctx context.Context, streams Streams, format string, actions LoginActions, preset service.LoginOptions, open func(string) error) (service.LoginResult, error) {
	steps := NewSteps(streams, format, loginTitles)
	form := newLoginForm(actions, preset)
	if err := runFlow(ctx, streams, steps, form.stages(), open); err != nil {
		return service.LoginResult{}, err
	}
	return form.result, nil
}

// loginForm is the state the login questions fill in.
type loginForm struct {
	actions    LoginActions
	options    service.LoginOptions
	presetURL  bool
	presetName bool
	saved      []service.SavedContext
	cloud      bool
	sameURL    []service.SavedContext // saved contexts with the checked URL
	replace    string                 // the context whose token is replaced
	check      service.LoginCheck
	result     service.LoginResult
}

func newLoginForm(actions LoginActions, preset service.LoginOptions) *loginForm {
	return &loginForm{actions: actions, options: preset, presetURL: preset.URL != "", presetName: preset.Name != "",
		saved: actions.SavedContexts(preset.ConfigPath)}
}

const (
	choiceSelfHosted = "self-hosted"
	choiceCloud      = "cloud"
	choiceNewName    = "new-name"
	replacePrefix    = "replace:"
)

func (f *loginForm) stages() []flowStage {
	return []flowStage{
		{step: loginType, kind: flowChoose, title: "Where does Coolify run?",
			note: f.savedNote,
			options: func() []flowOption {
				return []flowOption{
					{label: "Self-hosted", detail: "your own server", value: choiceSelfHosted},
					{label: "Coolify Cloud", detail: service.CloudURL, value: choiceCloud},
				}
			},
			initial: func() string { return choiceSelfHosted },
			set: func(value string) string {
				f.cloud = value == choiceCloud
				if f.cloud {
					return "Coolify Cloud"
				}
				return "Self-hosted"
			},
			skip: func() (string, bool) {
				if !f.presetURL {
					return "", false
				}
				if f.options.URL == service.CloudURL {
					return "Coolify Cloud", true
				}
				return "Self-hosted", true
			}},
		{step: loginURL, kind: flowText, title: "Coolify URL", placeholder: "https://coolify.example.com",
			note: func() []string { return []string{"The address you open Coolify at in a browser."} },
			validate: func(value string) error {
				if _, err := service.NormalizeInstanceURL(value); err != nil {
					return errors.New("Enter the full URL, such as https://coolify.example.com")
				}
				return nil
			},
			set: func(value string) string {
				f.options.URL, _ = service.NormalizeInstanceURL(value)
				return f.options.URL
			},
			skip: func() (string, bool) {
				switch {
				case f.presetURL:
					return f.options.URL, true
				case f.cloud:
					f.options.URL = service.CloudURL
					return service.CloudURL, true
				}
				return "", false
			}},
		{step: loginCheckInstance, kind: flowCheck,
			prepare: func() func(context.Context) (any, error) {
				address := f.options.URL
				return func(ctx context.Context) (any, error) { return f.actions.CheckInstance(ctx, address) }
			},
			accept: func(result any) string {
				f.options.URL = result.(string)
				f.sameURL = nil
				for _, saved := range f.saved {
					if saved.URL == f.options.URL {
						f.sameURL = append(f.sameURL, saved)
					}
				}
				return "Coolify answered"
			}},
		{step: loginName, kind: flowChoose,
			title: "This instance is already saved",
			note: func() []string {
				names := make([]string, len(f.sameURL))
				for i, saved := range f.sameURL {
					names[i] = saved.Name
				}
				return []string{f.options.URL + " is saved as " + strings.Join(names, ", ") + "."}
			},
			options: func() []flowOption {
				var options []flowOption
				for _, saved := range f.sameURL {
					options = append(options, flowOption{label: "Replace the token of " + saved.Name, value: replacePrefix + saved.Name})
				}
				return append(options, flowOption{label: "Save under a new name", value: choiceNewName}, flowOption{label: "Leave", leave: true})
			},
			set: func(value string) string {
				f.replace = strings.TrimPrefix(value, replacePrefix)
				if value == choiceNewName {
					f.replace = ""
					return "new name"
				}
				f.options.Name = f.replace
				return f.replace + " (new token)"
			},
			skip: func() (string, bool) {
				if f.presetName || len(f.sameURL) == 0 {
					f.replace = ""
					return "", true
				}
				return "", false
			}},
		{step: loginName, kind: flowText, title: "Context name",
			note: func() []string {
				return []string{"What Coolship and coolify-cli call this instance on this machine."}
			},
			initial: func() string { return service.SuggestContextName(f.options.URL, f.saved) },
			validate: func(value string) error {
				switch {
				case value == "":
					return errors.New("Enter a name")
				case strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0:
					return errors.New("A context name has no spaces")
				}
				for _, saved := range f.saved {
					if saved.Name == value {
						return errors.New(value + " is already saved for " + saved.URL + "; choose another name")
					}
				}
				return nil
			},
			set: func(value string) string {
				f.options.Name = value
				return value
			},
			skip: func() (string, bool) {
				switch {
				case f.presetName:
					return f.options.Name, true
				case f.replace != "":
					return f.replace + " (new token)", true
				}
				return "", false
			}},
		{step: loginToken, kind: flowSecret, title: "API token",
			note: func() []string {
				return []string{"Create one in Coolify under Keys & Tokens, API tokens: " + f.options.URL + "/security/api-tokens"}
			},
			validate: func(value string) error {
				if value == "" {
					return errors.New("Paste the token")
				}
				return nil
			},
			set: func(value string) string {
				f.options.Token = value
				return maskToken(value)
			}},
		{step: loginCheckToken, kind: flowCheck,
			prepare: func() func(context.Context) (any, error) {
				options := f.options
				return func(ctx context.Context) (any, error) { return f.actions.CheckLogin(ctx, options) }
			},
			accept: func(result any) string {
				f.check = result.(service.LoginCheck)
				return "team " + f.check.Team + " on Coolify " + f.check.Server
			}},
		{step: loginSave, kind: flowCheck,
			prepare: func() func(context.Context) (any, error) {
				options, check := f.options, f.check
				return func(ctx context.Context) (any, error) { return f.actions.SaveLogin(ctx, options, check) }
			},
			accept: func(result any) string {
				f.result = result.(service.LoginResult)
				return f.result.Path
			}},
	}
}

// savedNote lists the saved contexts under the first question, so it is
// clear what the machine already has; they are shown, not offered.
func (f *loginForm) savedNote() []string {
	if len(f.saved) == 0 {
		return nil
	}
	lines := []string{"Already saved on this machine:"}
	for _, saved := range f.saved {
		line := "  " + saved.Name + "  " + saved.URL
		if saved.Default {
			line += "  (default)"
		}
		lines = append(lines, line)
	}
	return lines
}

// maskToken shows only the last four characters, as debug output does.
func maskToken(token string) string {
	if len(token) <= 8 {
		return "••••"
	}
	return "••••" + token[len(token)-4:]
}
