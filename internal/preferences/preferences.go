// Package preferences reads one developer's tastes on one machine: the default
// verbosity, whether build logs stream, and color. They live in
// preferences.toml under the user's configuration directory, are never
// committed, and are never a project fact; the project's coolship.toml is
// internal/config's, and the Coolify CLI credentials file is internal/auth's.
// The file is only ever read, never created or written.
package preferences

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Verbosity values. The command tree uses the preference as the default when
// neither --verbose, --debug, nor COOLSHIP_VERBOSITY is given.
const (
	VerbosityNormal  = "normal"
	VerbosityVerbose = "verbose"
	VerbosityDebug   = "debug"
)

// Color values.
const (
	ColorAuto   = "auto"
	ColorAlways = "always"
	ColorNever  = "never"
)

var (
	verbosities = []string{VerbosityNormal, VerbosityVerbose, VerbosityDebug}
	colors      = []string{ColorAuto, ColorAlways, ColorNever}
)

// EnvPath is the environment variable that names the file to read instead of
// the default location; tests and CI use it.
const EnvPath = "COOLSHIP_PREFERENCES"

// Preferences is the file's content. The zero value is "no preference":
// an empty string or a nil BuildLogs means the key is absent and the
// consumer applies its own default, which for BuildLogs means following the
// verbosity.
type Preferences struct {
	Verbosity string `toml:"verbosity" json:"verbosity,omitempty"`
	BuildLogs *bool  `toml:"build_logs" json:"build_logs,omitempty"`
	Color     string `toml:"color" json:"color,omitempty"`
}

// Report is what Inspect found: where it looked, whether the file exists,
// what it holds, and, when it exists but cannot be used, why.
type Report struct {
	Path        string
	Present     bool
	Preferences Preferences
	Err         error
}

// Error reports a preferences file that exists but could not be read, parsed,
// or validated. Path names the file; Err says why and is one of the os
// errors, a decoding problem, or a *ValueError.
type Error struct {
	Path string
	Err  error
}

func (e *Error) Error() string { return "preferences file " + e.Path + ": " + e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

// ValueError is a key whose value is not one of those the key accepts.
type ValueError struct {
	Key     string
	Value   string
	Allowed []string
}

func (e *ValueError) Error() string {
	return fmt.Sprintf("%s must be one of %s, not %q", e.Key, strings.Join(e.Allowed, ", "), e.Value)
}

// Discover locates and inspects the file for this process's environment; it
// is what the executable calls once, before the command tree runs.
func Discover(env func(string) string) Report {
	path, err := Locate(env)
	if err != nil {
		return Report{Err: err}
	}
	return Inspect(path)
}

// Locate returns the file to read: COOLSHIP_PREFERENCES when set, otherwise
// the default location for this platform.
func Locate(env func(string) string) (string, error) {
	if env == nil {
		env = os.Getenv
	}
	if path := env(EnvPath); path != "" {
		return path, nil
	}
	return resolve(runtime.GOOS, env, os.UserHomeDir)
}

// DefaultPath is $XDG_CONFIG_HOME/coolship/preferences.toml when
// XDG_CONFIG_HOME is absolute, otherwise ~/.config/coolship/preferences.toml;
// on Windows it is %AppData%\coolship\preferences.toml. The home and AppData
// fallbacks follow auth.DefaultPath, but the two files are not the same file
// and share no code: credentials belong to Coolify CLI, preferences to
// Coolship alone.
func DefaultPath() (string, error) {
	return resolve(runtime.GOOS, os.Getenv, os.UserHomeDir)
}

func resolve(goos string, env func(string) string, userHome func() (string, error)) (string, error) {
	if goos == "windows" {
		if appData := env("APPDATA"); appData != "" {
			return filepath.Join(appData, "coolship", "preferences.toml"), nil
		}
		home, err := userHome()
		if err != nil {
			return "", fmt.Errorf("find preferences home: %w", err)
		}
		return filepath.Join(home, "AppData", "Roaming", "coolship", "preferences.toml"), nil
	}
	if xdg := env("XDG_CONFIG_HOME"); filepath.IsAbs(xdg) {
		return filepath.Join(xdg, "coolship", "preferences.toml"), nil
	}
	home, err := userHome()
	if err != nil {
		return "", fmt.Errorf("find preferences home: %w", err)
	}
	return filepath.Join(home, ".config", "coolship", "preferences.toml"), nil
}

// Load reads the file at path. A file that does not exist yields the zero
// value and no error; one that exists but cannot be read, parsed, or
// validated yields a *Error. The file is never created.
func Load(path string) (Preferences, error) {
	report := Inspect(path)
	return report.Preferences, report.Err
}

// Inspect is Load with the facts `coolship config` shows: whether the file
// exists, and the problem when it exists but cannot be used.
func Inspect(path string) Report {
	report := Report{Path: path}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return report
	}
	report.Present = true
	if err != nil {
		report.Err = &Error{Path: path, Err: err}
		return report
	}
	report.Preferences, err = Parse(data)
	if err != nil {
		report.Err = &Error{Path: path, Err: err}
	}
	return report
}

// Parse decodes the file strictly: an unknown key is an error naming the
// key, and each enumeration is checked against its allowed values.
func Parse(data []byte) (Preferences, error) {
	var value Preferences
	if err := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&value); err != nil {
		return Preferences{}, describeDecodeError(err)
	}
	if err := Validate(value); err != nil {
		return Preferences{}, err
	}
	return value, nil
}

// Validate checks every enumeration; an absent key is always valid.
func Validate(value Preferences) error {
	if err := oneOf("verbosity", value.Verbosity, verbosities); err != nil {
		return err
	}
	return oneOf("color", value.Color, colors)
}

func oneOf(key, value string, allowed []string) error {
	if value == "" {
		return nil
	}
	for _, candidate := range allowed {
		if value == candidate {
			return nil
		}
	}
	return &ValueError{Key: key, Value: value, Allowed: allowed}
}

// describeDecodeError turns go-toml's errors into one line that names what
// is wrong. The parser's source excerpts are left out: the message names the
// key and the line, which is enough to find a typo.
func describeDecodeError(err error) error {
	var strict *toml.StrictMissingError
	if errors.As(err, &strict) {
		keys := make([]string, 0, len(strict.Errors))
		for _, missing := range strict.Errors {
			keys = append(keys, fmt.Sprintf("%q", strings.Join(missing.Key(), ".")))
		}
		if len(keys) == 1 {
			return fmt.Errorf("unknown key %s; the keys are verbosity, build_logs, and color", keys[0])
		}
		return fmt.Errorf("unknown keys %s; the keys are verbosity, build_logs, and color", strings.Join(keys, ", "))
	}
	var decode *toml.DecodeError
	if errors.As(err, &decode) {
		line, _ := decode.Position()
		message := strings.TrimPrefix(decode.Error(), "toml: ")
		if key := strings.Join(decode.Key(), "."); key != "" {
			return fmt.Errorf("key %q on line %d: %s", key, line, message)
		}
		return fmt.Errorf("line %d: %s", line, message)
	}
	return err
}
