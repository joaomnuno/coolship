package cmd

import (
	"strings"

	"github.com/joaomnuno/coolship/internal/preferences"
	"github.com/spf13/cobra"
)

// This file is what `coolship completion <shell>` scripts ask at every Tab.
// Cobra completes command and flag names by itself; everything here is the
// values, and the rule for all of it is that a Tab costs nothing: these
// functions read local files at most, never the network and never a token, so
// a keystroke cannot hang on an unreachable instance, wake a sleeping laptop's
// VPN, or spend a rate limit. Names that only the server knows — projects,
// environments, applications, GitHub Apps, keys, deployment UUIDs — are
// therefore not offered.
//
// The other half is silence: a command that takes no argument says so with
// ShellCompDirectiveNoFileComp, so a Tab after `coolship status ` offers
// nothing instead of falling back to the shell's file list, which is what
// made completion look broken (#100). A command whose argument really is a
// path keeps the file list.

// noFileComp is the answer for a command that takes no positional argument:
// nothing to offer, and no file list either.
func noFileComp(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return nil, cobra.ShellCompDirectiveNoFileComp
}

// completeTargets offers the named targets of the discovered configuration for
// a target argument, and nothing at all for a single-target project, where the
// argument is not used. It is registered both for the positional argument and
// for --target.
func completeTargets(app Application, options *commandOptions) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(_ *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
		// The target is the first argument only; cancel's second is a
		// deployment UUID, which only the server knows.
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		if app == nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		var out []string
		for _, target := range app.CompletionTargets(options.Options) {
			if strings.HasPrefix(target.Name, prefix) {
				out = append(out, target.Name+"\t"+target.Purpose)
			}
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}

// completeContexts offers the saved Coolify contexts by name, read from the
// credentials file. Only names and URLs are read; a token never reaches a
// completion.
func completeContexts(app Application, options *commandOptions) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(_ *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
		if app == nil || len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		var out []string
		for _, context := range app.SavedContexts(options.CoolifyConfig) {
			if !strings.HasPrefix(context.Name, prefix) {
				continue
			}
			description := context.URL
			if context.Default {
				description += " (default)"
			}
			out = append(out, context.Name+"\t"+description)
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}

// completePreferenceKeys offers every preference key with its description, from
// the one list that describes them.
func completePreferenceKeys(_ *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var out []string
	for _, key := range preferences.Keys() {
		if strings.HasPrefix(key.Name, prefix) {
			out = append(out, key.Name+"\t"+key.Description)
		}
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

// completePreferenceSetArgs completes `config set KEY VALUE`: the keys, then
// the values that key accepts, which the key's own descriptor lists.
func completePreferenceSetArgs(_ *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
	switch len(args) {
	case 0:
		return completePreferenceKeys(nil, nil, prefix)
	case 1:
		key, ok := preferences.Lookup(args[0])
		if !ok {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return withPrefix(key.Allowed, prefix), cobra.ShellCompDirectiveNoFileComp
	default:
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
}

// fixedValues is the completion for a flag whose values are a closed list this
// binary already knows, such as --format or --build-pack.
func fixedValues(values ...string) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(_ *cobra.Command, _ []string, prefix string) ([]string, cobra.ShellCompDirective) {
		return withPrefix(values, prefix), cobra.ShellCompDirectiveNoFileComp
	}
}

// completeDirectories keeps the shell's own directory list for a flag that
// names one, instead of offering every file beside it.
func completeDirectories(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return nil, cobra.ShellCompDirectiveFilterDirs
}

func withPrefix(values []string, prefix string) []string {
	var out []string
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			out = append(out, value)
		}
	}
	return out
}

// registerCompletions wires every value completion onto the tree, after the
// commands are built. It walks the tree so a command added later cannot
// silently keep the file-list fallback: anything that takes no argument and
// was not given a function gets the silent one.
func registerCompletions(root *cobra.Command, app Application, options *commandOptions) {
	targets := completeTargets(app, options)
	contexts := completeContexts(app, options)

	registerFlag(root, "target", targets)
	registerFlag(root, "context", contexts)
	registerFlag(root, "format", fixedValues("human", "json"))
	registerFlag(root, "cwd", completeDirectories)

	for _, command := range allCommands(root) {
		if command == root {
			// Cobra completes the root's own subcommand names; a function
			// here would only compete with them.
			continue
		}
		switch commandPath(root, command) {
		case "logout":
			command.ValidArgsFunction = contexts
		case "config get":
			command.ValidArgsFunction = completePreferenceKeys
		case "config set":
			command.ValidArgsFunction = completePreferenceSetArgs
		case "init":
			registerFlag(command, "build-pack", fixedValues(buildPackNames...))
			registerFlag(command, "source", fixedValues(initSourceNames...))
		case "dev":
			// dev runs a local command: the file list is the right answer.
			continue
		case "completion", "completion bash", "completion zsh", "completion fish", "completion powershell", "help":
			// Cobra completes the shells and the command names itself.
			continue
		}
		if command.ValidArgsFunction != nil {
			continue
		}
		if takesTarget(command) {
			command.ValidArgsFunction = targets
			continue
		}
		command.ValidArgsFunction = noFileComp
	}
}

// takesTarget reports whether a command's first argument is a monorepo target,
// which its own Use line is the declaration of.
func takesTarget(command *cobra.Command) bool {
	return strings.Contains(command.Use, "[target]")
}

// registerFlag attaches a completion to one flag, when the command has it.
// Cobra refuses a second registration for the same flag, so this is called
// once per flag.
func registerFlag(command *cobra.Command, name string, f func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective)) {
	if command.Flags().Lookup(name) == nil && command.PersistentFlags().Lookup(name) == nil {
		return
	}
	// The error is only ever "no such flag", which the lookup above ruled out.
	_ = command.RegisterFlagCompletionFunc(name, f)
}

// allCommands is every command in the tree, the root included.
func allCommands(root *cobra.Command) []*cobra.Command {
	out := []*cobra.Command{root}
	for _, child := range root.Commands() {
		out = append(out, allCommands(child)...)
	}
	return out
}

// commandPath is a command's path without the binary's name, so a switch on it
// reads as the command a developer types.
func commandPath(root, command *cobra.Command) string {
	return strings.TrimPrefix(strings.TrimPrefix(command.CommandPath(), root.Name()), " ")
}

// buildPackNames and initSourceNames are what init's own help documents, kept
// beside the flags they complete.
var (
	buildPackNames  = []string{"railpack", "nixpacks", "static", "dockerfile", "dockercompose"}
	initSourceNames = []string{"auto", "public", "github-app", "deploy-key"}
)
