package auth

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFindLoginMatchesTheURLAndTellsWhichContextHoldsTheToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if match, err := FindLogin(path, "https://c.example.com", "t"); err != nil || len(match.Names) != 0 || match.Duplicate != "" {
		t.Fatalf("missing file: %+v %v", match, err)
	}
	document := `{"instances":[
		{"name":"home","fqdn":"https://C.example.com/","token":"old"},
		{"name":"lab","fqdn":"https://lab.example.com","token":"new"},
		{"name":"again","fqdn":"https://c.example.com","token":"new"},
		{"fqdn":"https://c.example.com","token":"new"}]}`
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	match, err := FindLogin(path, "https://c.example.com", " new ")
	if err != nil || !reflect.DeepEqual(match.Names, []string{"home", "again"}) || match.Duplicate != "again" {
		t.Fatalf("match = %+v, err = %v", match, err)
	}
	if match, _ := FindLogin(path, "https://c.example.com", "other"); match.Duplicate != "" || len(match.Names) != 2 {
		t.Fatalf("other token: %+v", match)
	}
	if _, err := FindLogin(path, "c.example.com", "new"); err == nil {
		t.Fatal("a bare host was accepted")
	}
	if text := (&DuplicateLoginError{Name: "again", URL: "https://c.example.com"}).Error(); text != `https://c.example.com and this token are already saved as context "again"` {
		t.Fatal(text)
	}
}
