// Package auth reads Coolify CLI credentials without changing their storage.
package auth

import (
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/joaomnuno/coolship/internal/suggest"
)

var (
	ErrInvalid         = errors.New("invalid Coolify credentials")
	ErrContextNotFound = errors.New("Coolify context not found")
)

type Options struct {
	Context    string
	ConfigPath string
	URL        string
	Token      string `json:"-" toml:"-"`
}

type Credentials struct {
	Name  string `json:"name" toml:"name"`
	URL   string `json:"url" toml:"url"`
	Token string `json:"-" toml:"-"`
}

// String and GoString prevent accidental token disclosure through fmt diagnostics.
func (c Credentials) String() string {
	return fmt.Sprintf("Credentials{Name:%q URL:%q Token:[redacted]}", c.Name, c.URL)
}

func (c Credentials) GoString() string { return c.String() }

type Instance struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Default bool   `json:"default"`
}

func Resolve(options Options) (Credentials, error) {
	if value, supplied, err := explicitPair(options); supplied || err != nil {
		return value, err
	}
	instances, err := load(options.ConfigPath)
	if err != nil {
		return Credentials{}, err
	}
	for _, instance := range instances {
		if (options.Context != "" && instance.Name == options.Context) || (options.Context == "" && instance.Default) {
			return Credentials{Name: instance.Name, URL: instance.FQDN, Token: instance.Token}, nil
		}
	}
	if options.Context != "" {
		return Credentials{}, contextNotFound(options.Context, instanceNames(instances))
	}
	return Credentials{}, fmt.Errorf("%w: no default instance; pass --context NAME or run coolship login --default", ErrInvalid)
}

// List exposes context identities for selection, with no credential fields.
func List(options Options) ([]Instance, error) {
	if value, supplied, err := explicitPair(options); supplied || err != nil {
		if err != nil {
			return nil, err
		}
		return []Instance{{Name: value.Name, URL: value.URL, Default: true}}, nil
	}
	instances, err := load(options.ConfigPath)
	if err != nil {
		return nil, err
	}
	result := make([]Instance, 0, len(instances))
	found := options.Context == ""
	for _, instance := range instances {
		result = append(result, Instance{Name: instance.Name, URL: instance.FQDN, Default: instance.Default})
		found = found || instance.Name == options.Context
	}
	if !found {
		return nil, contextNotFound(options.Context, instanceNames(instances))
	}
	return result, nil
}

// contextNotFound names the saved contexts, and the one name was probably
// meant to be, so a typo is fixed without opening the credentials file. Only
// names are listed: never a URL or a token.
func contextNotFound(name string, names []string) error {
	if len(names) == 0 {
		return fmt.Errorf("%w: %q; no contexts are saved, run coolship login", ErrContextNotFound, name)
	}
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	return fmt.Errorf("%w: %q%s; saved contexts: %s", ErrContextNotFound, name, suggest.DidYouMean(name, sorted), strings.Join(sorted, ", "))
}

func instanceNames(instances []storedInstance) []string {
	names := make([]string, 0, len(instances))
	for _, instance := range instances {
		names = append(names, instance.Name)
	}
	return names
}

func explicitPair(options Options) (Credentials, bool, error) {
	if options.URL == "" && options.Token == "" {
		return Credentials{}, false, nil
	}
	if options.URL == "" || strings.TrimSpace(options.Token) == "" {
		return Credentials{}, true, fmt.Errorf("%w: COOLSHIP_URL and COOLSHIP_TOKEN must be supplied together", ErrInvalid)
	}
	if options.Context != "" || options.ConfigPath != "" {
		return Credentials{}, true, fmt.Errorf("%w: COOLSHIP_URL/COOLSHIP_TOKEN cannot be combined with --context or --coolify-config", ErrInvalid)
	}
	address, err := normalizeURL(options.URL)
	if err != nil {
		return Credentials{}, true, err
	}
	if strings.ContainsAny(options.Token, "\r\n") {
		return Credentials{}, true, fmt.Errorf("%w: token must not contain line breaks", ErrInvalid)
	}
	return Credentials{Name: "environment", URL: address, Token: options.Token}, true, nil
}

func normalizeURL(value string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed == nil || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" ||
		parsed.ForceQuery || parsed.Fragment != "" || parsed.Opaque != "" {
		// Never include the original URL: it may contain embedded credentials.
		return "", fmt.Errorf("%w: instance URL must be an absolute HTTP(S) URL without credentials, query, or fragment", ErrInvalid)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = strings.TrimRight(parsed.RawPath, "/")
	return parsed.String(), nil
}
