// Package envfile reads and writes dotenv files while preserving everything it
// does not own: comments, blank lines, ordering, and unknown syntax.
package envfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

var ErrSyntax = errors.New("invalid dotenv syntax")

// Entry is one variable as read from a file.
type Entry struct {
	Key   string
	Value string
	Line  int
}

// File keeps the original lines so an update rewrites only the values it sets.
type File struct {
	lines   []string
	entries map[string]int // key -> line index
	order   []string
	Mode    os.FileMode
	Exists  bool
}

// Parse accepts KEY=VALUE, export KEY=VALUE, comments, blank lines, and single
// or double quoted values. Double quotes allow \n, \t, \", \\ escapes and may
// span lines. No interpolation is performed. Later duplicates win, as in most
// dotenv loaders, but each is reported.
func Parse(data []byte) (*File, error) {
	f := &File{entries: map[string]int{}, Mode: 0o600}
	f.lines = strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(data) == 0 {
		f.lines = nil
	}
	for i := 0; i < len(f.lines); i++ {
		key, _, consumed, err := parseLine(f.lines, i)
		if err != nil {
			return nil, fmt.Errorf("%w at line %d: %v", ErrSyntax, i+1, err)
		}
		if key != "" {
			if _, seen := f.entries[key]; !seen {
				f.order = append(f.order, key)
			}
			f.entries[key] = i
		}
		i += consumed
	}
	return f, nil
}

// Read loads path. A missing file is an empty File with Exists false.
func Read(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		f, _ := Parse(nil)
		return f, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	f, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	f.Exists = true
	if info, err := os.Stat(path); err == nil {
		f.Mode = info.Mode().Perm()
	}
	return f, nil
}

// Entries returns variables in first-appearance order.
func (f *File) Entries() []Entry {
	result := make([]Entry, 0, len(f.order))
	for _, key := range f.order {
		index := f.entries[key]
		_, value, _, _ := parseLine(f.lines, index)
		result = append(result, Entry{Key: key, Value: value, Line: index + 1})
	}
	return result
}

// Get returns a value and whether the key is present.
func (f *File) Get(key string) (string, bool) {
	index, ok := f.entries[key]
	if !ok {
		return "", false
	}
	_, value, _, _ := parseLine(f.lines, index)
	return value, true
}

// Set replaces a key's value in place, or appends the key. A multi-line
// quoted value is collapsed to one formatted line.
func (f *File) Set(key, value string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	formatted := key + "=" + Format(value)
	if index, ok := f.entries[key]; ok {
		_, _, consumed, _ := parseLine(f.lines, index)
		f.lines = append(f.lines[:index], append([]string{formatted}, f.lines[index+1+consumed:]...)...)
		if consumed > 0 {
			f.reindex()
		}
		return nil
	}
	f.lines = append(f.lines, formatted)
	f.entries[key] = len(f.lines) - 1
	f.order = append(f.order, key)
	return nil
}

// Comment appends a comment line, used to record keys whose values are
// unavailable rather than inventing a value for them.
func (f *File) Comment(text string) {
	f.lines = append(f.lines, "# "+strings.ReplaceAll(strings.TrimSpace(text), "\n", " "))
}

// Delete removes a key's line.
func (f *File) Delete(key string) bool {
	index, ok := f.entries[key]
	if !ok {
		return false
	}
	_, _, consumed, _ := parseLine(f.lines, index)
	f.lines = append(f.lines[:index], f.lines[index+1+consumed:]...)
	f.reindex()
	return true
}

func (f *File) reindex() {
	f.entries = map[string]int{}
	f.order = nil
	for i := 0; i < len(f.lines); i++ {
		key, _, consumed, err := parseLine(f.lines, i)
		if err == nil && key != "" {
			if _, seen := f.entries[key]; !seen {
				f.order = append(f.order, key)
			}
			f.entries[key] = i
		}
		i += consumed
	}
}

// Bytes renders the file with a trailing newline when it has content.
func (f *File) Bytes() []byte {
	if len(f.lines) == 0 {
		return nil
	}
	return []byte(strings.Join(f.lines, "\n") + "\n")
}

// Write replaces path atomically, creating it with mode 0600 and otherwise
// keeping the existing permissions. Variables are often secrets.
func (f *File) Write(path string) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".env-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	defer os.Remove(temporary.Name())
	mode := os.FileMode(0o600)
	if f.Exists && f.Mode != 0 {
		mode = f.Mode
	}
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return fmt.Errorf("set permissions: %w", err)
	}
	if _, err := temporary.Write(f.Bytes()); err != nil {
		temporary.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync %s: %w", path, err)
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary.Name(), path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	f.Exists = true
	f.Mode = mode
	return nil
}

// ValidateKey accepts the POSIX environment name form Coolify also accepts.
func ValidateKey(key string) error {
	if key == "" {
		return fmt.Errorf("%w: empty key", ErrSyntax)
	}
	for i, r := range key {
		if r == '_' || unicode.IsLetter(r) && r < 128 || (i > 0 && unicode.IsDigit(r) && r < 128) {
			continue
		}
		return fmt.Errorf("%w: key %q must match [A-Za-z_][A-Za-z0-9_]*", ErrSyntax, key)
	}
	return nil
}

// Format quotes a value only when it would otherwise be misread.
func Format(value string) string {
	if value == "" {
		return ""
	}
	needsQuotes := strings.ContainsAny(value, " \t\n\r\"'#$`\\") || value != strings.TrimSpace(value)
	if !needsQuotes {
		return value
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// parseLine returns the key and value at index, and how many extra lines a
// quoted value consumed. Non-assignment lines return an empty key.
func parseLine(lines []string, index int) (key, value string, consumed int, err error) {
	line := lines[index]
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", 0, nil
	}
	trimmed = strings.TrimPrefix(trimmed, "export ")
	equals := strings.IndexByte(trimmed, '=')
	if equals <= 0 {
		return "", "", 0, errors.New("expected KEY=VALUE")
	}
	key = strings.TrimSpace(trimmed[:equals])
	if err := ValidateKey(key); err != nil {
		return "", "", 0, err
	}
	rest := strings.TrimLeftFunc(trimmed[equals+1:], unicode.IsSpace)
	switch {
	case strings.HasPrefix(rest, `"`):
		value, consumed, err = parseDoubleQuoted(lines, index, rest[1:])
		return key, value, consumed, err
	case strings.HasPrefix(rest, `'`):
		end := strings.IndexByte(rest[1:], '\'')
		if end < 0 {
			return "", "", 0, errors.New("unterminated single quote")
		}
		return key, rest[1 : 1+end], 0, trailing(rest[2+end:])
	default:
		if hash := strings.Index(rest, " #"); hash >= 0 {
			rest = rest[:hash]
		}
		return key, strings.TrimRightFunc(rest, unicode.IsSpace), 0, nil
	}
}

func parseDoubleQuoted(lines []string, index int, rest string) (string, int, error) {
	var b strings.Builder
	consumed := 0
	for {
		for i := 0; i < len(rest); i++ {
			switch rest[i] {
			case '\\':
				if i+1 >= len(rest) {
					return "", 0, errors.New("dangling escape")
				}
				i++
				switch rest[i] {
				case 'n':
					b.WriteByte('\n')
				case 'r':
					b.WriteByte('\r')
				case 't':
					b.WriteByte('\t')
				case '"', '\\', '$':
					b.WriteByte(rest[i])
				default:
					b.WriteByte('\\')
					b.WriteByte(rest[i])
				}
			case '"':
				return b.String(), consumed, trailing(rest[i+1:])
			default:
				b.WriteByte(rest[i])
			}
		}
		consumed++
		if index+consumed >= len(lines) {
			return "", 0, errors.New("unterminated double quote")
		}
		b.WriteByte('\n')
		rest = lines[index+consumed]
	}
}

func trailing(rest string) error {
	rest = strings.TrimSpace(rest)
	if rest == "" || strings.HasPrefix(rest, "#") {
		return nil
	}
	return fmt.Errorf("unexpected text after value: %q", rest)
}
