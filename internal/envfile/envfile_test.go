package envfile

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseAcceptsCommonDotenvForms(t *testing.T) {
	data := `# comment
PLAIN=hello
export EXPORTED=yes
SPACED = value with spaces # trailing comment
SINGLE='it is # not a comment'
DOUBLE="line1\nline2 \"quoted\" \\ $literal"
MULTI="first
second"
EMPTY=
DUP=1
DUP=2
`
	f, err := Parse([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"PLAIN": "hello", "EXPORTED": "yes", "SPACED": "value with spaces",
		"SINGLE": "it is # not a comment", "DOUBLE": "line1\nline2 \"quoted\" \\ $literal", "MULTI": "first\nsecond", "EMPTY": "", "DUP": "2",
	}
	for key, value := range want {
		got, ok := f.Get(key)
		if !ok || got != value {
			t.Errorf("%s = %q (present=%t), want %q", key, got, ok, value)
		}
	}
	// A single-quoted value ends at the first quote; the rest must be rejected.
	if _, err := Parse([]byte("X='a'b\n")); !errors.Is(err, ErrSyntax) {
		t.Errorf("trailing garbage accepted: %v", err)
	}
	for _, bad := range []string{"NOEQUALS\n", "1BAD=x\n", "X=\"unterminated\n", "BAD KEY=x\n"} {
		if _, err := Parse([]byte(bad)); !errors.Is(err, ErrSyntax) {
			t.Errorf("%q accepted: %v", bad, err)
		}
	}
}

func TestSetPreservesCommentsOrderAndUnrelatedLines(t *testing.T) {
	f, err := Parse([]byte("# keep me\nA=1\n\nB=\"two\nlines\"\nC=3\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Set("B", "single"); err != nil {
		t.Fatal(err)
	}
	if err := f.Set("D", "new value"); err != nil {
		t.Fatal(err)
	}
	f.Comment("E is withheld")
	got := string(f.Bytes())
	want := "# keep me\nA=1\n\nB=single\nC=3\nD=\"new value\"\n# E is withheld\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if !f.Delete("A") || f.Delete("A") {
		t.Fatal("delete semantics")
	}
	keys := []string{}
	for _, entry := range f.Entries() {
		keys = append(keys, entry.Key)
	}
	if !reflect.DeepEqual(keys, []string{"B", "C", "D"}) {
		t.Fatalf("order after delete: %v", keys)
	}
	if err := f.Set("bad key", "x"); !errors.Is(err, ErrSyntax) {
		t.Fatal("invalid key accepted")
	}
}

func TestFormatRoundTrips(t *testing.T) {
	for _, value := range []string{"", "plain", "with space", "hash # inside", "quote \" and ' both", "multi\nline", "tab\tted", "back\\slash", "$dollar", " padded "} {
		f, _ := Parse(nil)
		if err := f.Set("K", value); err != nil {
			t.Fatal(err)
		}
		parsed, err := Parse(f.Bytes())
		if err != nil {
			t.Fatalf("%q: %v (%s)", value, err, f.Bytes())
		}
		if got, _ := parsed.Get("K"); got != value {
			t.Errorf("round trip %q -> %q via %s", value, got, f.Bytes())
		}
	}
}

func TestWriteCreatesPrivateFileAndKeepsExistingMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	f, err := Read(path)
	if err != nil || f.Exists {
		t.Fatalf("missing file: exists=%t err=%v", f.Exists, err)
	}
	if err := f.Set("SECRET", "s"); err != nil {
		t.Fatal(err)
	}
	if err := f.Write(path); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("new file mode %o", info.Mode().Perm())
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	f, err = Read(path)
	if err != nil || !f.Exists {
		t.Fatal(err)
	}
	if err := f.Set("SECRET", "t"); err != nil {
		t.Fatal(err)
	}
	if err := f.Write(path); err != nil {
		t.Fatal(err)
	}
	info, _ = os.Stat(path)
	data, _ := os.ReadFile(path)
	if info.Mode().Perm() != 0o640 || !strings.Contains(string(data), "SECRET=t") {
		t.Fatalf("mode %o content %q", info.Mode().Perm(), data)
	}
	if entries, _ := filepath.Glob(filepath.Join(dir, ".env-*.tmp")); len(entries) != 0 {
		t.Fatal("temporary file left behind")
	}
}
