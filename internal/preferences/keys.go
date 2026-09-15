package preferences

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Kind is how a key's value is written in the file.
type Kind string

const (
	// KindString is one of Allowed, written as a quoted TOML string.
	KindString Kind = "string"
	// KindBool is true or false, written as a TOML boolean.
	KindBool Kind = "bool"
	// KindOptionalBool is auto, true, or false: true and false are written
	// as a TOML boolean, and auto removes the key so the consumer decides.
	KindOptionalBool Kind = "optional_bool"
)

// Auto is the value of a KindOptionalBool key the file does not set.
const Auto = "auto"

// Key describes one preference: its name in the file, how it is written,
// the values it accepts, what it does, and the value in effect when the
// file does not set it. config get, config set, and the config form all
// work from this list, so a new key needs only an entry here and a field.
type Key struct {
	Name        string
	Kind        Kind
	Allowed     []string
	Description string
	Default     string
}

// Keys returns every preference in the order the config form shows them.
func Keys() []Key {
	return []Key{
		{Name: "verbosity", Kind: KindString, Allowed: slices.Clone(verbosities), Default: VerbosityNormal,
			Description: "Default output detail when neither --verbose nor --debug is given"},
		{Name: "build_logs", Kind: KindOptionalBool, Allowed: []string{Auto, "true", "false"}, Default: Auto,
			Description: "Stream build logs during a deployment; auto follows the verbosity"},
		{Name: "color", Kind: KindString, Allowed: slices.Clone(colors), Default: ColorAuto,
			Description: "Styled output; auto colors a terminal and never a pipe"},
		{Name: "hints", Kind: KindBool, Allowed: []string{"true", "false"}, Default: "true",
			Description: "Next-step hints, the questions after them, and offers to fix a failed command"},
		{Name: "update_check", Kind: KindBool, Allowed: []string{"true", "false"}, Default: "true",
			Description: "Check once a day for a newer Coolship release"},
	}
}

// Lookup returns the descriptor for name.
func Lookup(name string) (Key, bool) {
	for _, key := range Keys() {
		if key.Name == name {
			return key, true
		}
	}
	return Key{}, false
}

// Names lists every key name, in the order Keys returns them.
func Names() []string {
	keys := Keys()
	names := make([]string, len(keys))
	for i, key := range keys {
		names[i] = key.Name
	}
	return names
}

// KeyError is a name that is not a preference.
type KeyError struct{ Key string }

func (e *KeyError) Error() string {
	return fmt.Sprintf("unknown preference %q; the keys are %s", e.Key, strings.Join(Names(), ", "))
}

// Check returns the descriptor for name after checking value against it.
func Check(name, value string) (Key, error) {
	key, ok := Lookup(name)
	if !ok {
		return Key{}, &KeyError{Key: name}
	}
	if !slices.Contains(key.Allowed, value) {
		return Key{}, &ValueError{Key: name, Value: value, Allowed: key.Allowed}
	}
	return key, nil
}

// Get returns the value in effect for name, as config get prints it, and
// whether the file sets it; an unset key reports its default.
func (p Preferences) Get(name string) (string, bool, error) {
	key, ok := Lookup(name)
	if !ok {
		return "", false, &KeyError{Key: name}
	}
	var text string
	var set bool
	switch name {
	case "verbosity":
		text, set = p.Verbosity, p.Verbosity != ""
	case "color":
		text, set = p.Color, p.Color != ""
	case "build_logs":
		text, set = formatBool(p.BuildLogs)
	case "update_check":
		text, set = formatBool(p.UpdateCheck)
	case "hints":
		text, set = formatBool(p.Hints)
	}
	if !set {
		text = key.Default
	}
	return text, set, nil
}

func formatBool(value *bool) (string, bool) {
	if value == nil {
		return "", false
	}
	return strconv.FormatBool(*value), true
}

// Change is one key to write and its new value, which must be one of the
// key's Allowed values.
type Change struct {
	Key   string
	Value string
}

// Apply writes changes into the file at path and nothing else: every other
// line, comments included, is kept as it was, a changed key keeps its
// place and its trailing comment, and a new key is added at the end.
// Setting a KindOptionalBool key to auto removes its line. The file and its
// directory are created when absent, with modes 0600 and 0700, and the new
// content replaces the old in one rename, so a reader never sees half a
// file. When the result would not parse, because of a problem elsewhere in
// the file, nothing is written and the *Error says why.
func Apply(path string, changes []Change) error {
	if path == "" {
		return errors.New("no preferences file location; set COOLSHIP_PREFERENCES")
	}
	for _, change := range changes {
		if _, err := Check(change.Key, change.Value); err != nil {
			return err
		}
	}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return &Error{Path: path, Err: err}
	}
	for _, change := range changes {
		key, _ := Lookup(change.Key)
		data = edit(data, key, change.Value)
	}
	if _, err := Parse(data); err != nil {
		return &Error{Path: path, Err: invalidFile{err}}
	}
	return writeAtomically(path, data)
}

// ErrInvalidFile matches an Apply failure caused by the file's content
// rather than by reading or writing it: the file is the user's to fix.
var ErrInvalidFile = errors.New("invalid preferences file")

type invalidFile struct{ err error }

func (e invalidFile) Error() string        { return e.err.Error() }
func (e invalidFile) Unwrap() error        { return e.err }
func (e invalidFile) Is(target error) bool { return target == ErrInvalidFile }

// edit sets one key in the file's text. The file holds top-level keys only
// (Parse refuses anything else), so the key is found by its line.
func edit(data []byte, key Key, value string) []byte {
	remove := key.Kind == KindOptionalBool && value == Auto
	literal := value
	if key.Kind == KindString {
		literal = strconv.Quote(value)
	}
	lines := strings.SplitAfter(string(data), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for i, line := range lines {
		equals, ok := keyLine(line, key.Name)
		if !ok {
			continue
		}
		if remove {
			return []byte(strings.Join(slices.Delete(lines, i, i+1), ""))
		}
		lines[i] = replaceValue(line, equals, literal)
		return []byte(strings.Join(lines, ""))
	}
	if remove {
		return data
	}
	text := strings.Join(lines, "")
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return []byte(text + key.Name + " = " + literal + "\n")
}

// keyLine reports whether line assigns name, bare or quoted, and where its
// equals sign is.
func keyLine(line, name string) (int, bool) {
	trimmed := strings.TrimLeft(line, " \t")
	offset := len(line) - len(trimmed)
	for _, form := range []string{name, `"` + name + `"`, "'" + name + "'"} {
		rest, ok := strings.CutPrefix(trimmed, form)
		if !ok {
			continue
		}
		after := strings.TrimLeft(rest, " \t")
		if strings.HasPrefix(after, "=") {
			return offset + len(form) + (len(rest) - len(after)), true
		}
	}
	return 0, false
}

// replaceValue swaps the value after the equals sign at equals for literal,
// keeping the spacing around the old value and any comment after it.
func replaceValue(line string, equals int, literal string) string {
	head, rest := line[:equals+1], line[equals+1:]
	newline := ""
	if trimmed, ok := strings.CutSuffix(rest, "\n"); ok {
		rest, newline = trimmed, "\n"
		if trimmed, ok := strings.CutSuffix(rest, "\r"); ok {
			rest, newline = trimmed, "\r\n"
		}
	}
	value, comment := rest, ""
	if index := commentStart(rest); index >= 0 {
		value, comment = rest[:index], rest[index:]
	}
	leading := value[:len(value)-len(strings.TrimLeft(value, " \t"))]
	if leading == "" {
		leading = " "
	}
	trailing := value[len(strings.TrimRight(value, " \t")):]
	if comment != "" && trailing == "" {
		trailing = " "
	}
	return head + leading + literal + trailing + comment + newline
}

// commentStart finds the # that starts a comment, skipping any inside a
// basic or literal string.
func commentStart(text string) int {
	var quote byte
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case quote == '"' && c == '\\':
			i++
		case quote != 0 && c == quote:
			quote = 0
		case quote != 0:
		case c == '"' || c == '\'':
			quote = c
		case c == '#':
			return i
		}
	}
	return -1
}

func writeAtomically(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return &Error{Path: path, Err: err}
	}
	file, err := os.CreateTemp(dir, ".preferences-*.toml")
	if err != nil {
		return &Error{Path: path, Err: err}
	}
	temporary := file.Name()
	fail := func(err error) error {
		_ = file.Close()
		_ = os.Remove(temporary)
		return &Error{Path: path, Err: err}
	}
	if err := file.Chmod(0o600); err != nil {
		return fail(err)
	}
	if _, err := file.Write(data); err != nil {
		return fail(err)
	}
	if err := file.Sync(); err != nil {
		return fail(err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(temporary)
		return &Error{Path: path, Err: err}
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return &Error{Path: path, Err: err}
	}
	return nil
}
