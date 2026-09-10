package auth

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveCreatesMergesAndKeepsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	written, err := Save(path, Stored{Name: "home", URL: "https://Coolify.Example.com/", Token: "tok-1"}, false)
	if err != nil || written != path {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
	instances, err := load(path)
	if err != nil || len(instances) != 1 || !instances[0].Default || instances[0].FQDN != "https://coolify.example.com" {
		t.Fatalf("first instance: %+v err=%v", instances, err)
	}
	// Coolify CLI's own fields survive a second save; the second instance is not default.
	var document map[string]any
	data, _ := os.ReadFile(path)
	json.Unmarshal(data, &document)
	document["lastUpdateCheckTime"] = "2026-01-01T00:00:00Z"
	document["instances"].([]any)[0].(map[string]any)["extra"] = "kept"
	data, _ = json.Marshal(document)
	os.WriteFile(path, data, 0o600)
	if _, err := Save(path, Stored{Name: "work", URL: "https://work.example.com", Token: "tok-2"}, false); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if !strings.Contains(string(data), `"lastUpdateCheckTime": "2026-01-01T00:00:00Z"`) || !strings.Contains(string(data), `"extra": "kept"`) {
		t.Fatalf("unknown fields lost:\n%s", data)
	}
	instances, _ = load(path)
	if len(instances) != 2 || !instances[0].Default || instances[1].Default {
		t.Fatalf("defaults after second save: %+v", instances)
	}
	// Re-saving an existing name replaces its token; --default moves the default.
	if _, err := Save(path, Stored{Name: "work", URL: "https://work.example.com", Token: "tok-3"}, true); err != nil {
		t.Fatal(err)
	}
	instances, _ = load(path)
	if len(instances) != 2 || instances[0].Default || !instances[1].Default || instances[1].Token != "tok-3" {
		t.Fatalf("after default switch: %+v", instances)
	}
	// Removing the default reports it; removing an unknown name fails.
	if _, wasDefault, err := Remove(path, "work"); err != nil || !wasDefault {
		t.Fatalf("remove: default=%t err=%v", wasDefault, err)
	}
	if _, _, err := Remove(path, "nope"); !errors.Is(err, ErrContextNotFound) {
		t.Fatalf("remove unknown: %v", err)
	}
	for _, bad := range []Stored{{Name: "", URL: "https://x", Token: "t"}, {Name: "a b", URL: "https://x", Token: "t"}, {Name: "x", URL: "ftp://x", Token: "t"}, {Name: "x", URL: "https://x", Token: ""}} {
		if _, err := Save(path, bad, false); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v accepted", bad)
		}
	}
	if _, err := Save(filepath.Join(t.TempDir(), "broken.json"), Stored{Name: "x", URL: "https://x.example.com", Token: "t"}, false); err != nil {
		t.Fatal(err)
	}
}

func TestSaveRefusesUnparseableFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(path, []byte("not json"), 0o600)
	if _, err := Save(path, Stored{Name: "x", URL: "https://x.example.com", Token: "t"}, false); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unparseable file overwritten: %v", err)
	}
}
