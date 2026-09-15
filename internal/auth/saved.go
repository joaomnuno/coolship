package auth

import (
	"fmt"
	"strings"
)

// Match is what the credentials file already holds for one instance URL:
// the contexts saved with that URL, and the one of them, if any, that also
// holds a given token. No token leaves this package.
type Match struct {
	Names     []string // contexts whose URL is the one asked about, in file order
	Duplicate string   // the context among them holding the same token, or ""
}

// FindLogin reads the Coolify CLI configuration the way Save does: a missing
// or empty file holds nothing, and entries this package cannot read are
// skipped. URLs are compared after normalization.
func FindLogin(path, address, token string) (Match, error) {
	address, err := normalizeURL(address)
	if err != nil {
		return Match{}, err
	}
	_, _, instances, err := readDocument(path)
	if err != nil {
		return Match{}, err
	}
	token = strings.TrimSpace(token)
	var match Match
	for _, instance := range instances {
		name, _ := instance["name"].(string)
		fqdn, _ := instance["fqdn"].(string)
		if name == "" {
			continue
		}
		if normalized, err := normalizeURL(fqdn); err != nil || normalized != address {
			continue
		}
		match.Names = append(match.Names, name)
		if saved, _ := instance["token"].(string); token != "" && match.Duplicate == "" && strings.TrimSpace(saved) == token {
			match.Duplicate = name
		}
	}
	return match, nil
}

// DuplicateLoginError refuses a login that would save a URL and token the
// file already holds under Name: there is nothing to change.
type DuplicateLoginError struct {
	Name string
	URL  string
}

func (e *DuplicateLoginError) Error() string {
	return fmt.Sprintf("%s and this token are already saved as context %q", e.URL, e.Name)
}
