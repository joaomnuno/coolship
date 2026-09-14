// Package update tells a developer that a newer Coolship release exists.
//
// It follows gh's update notifier: the latest GitHub release is checked at
// most once a day, in the background while the command runs, and a check
// still running when the command ends is abandoned rather than waited for.
// Like Railway's, the notice comes from the cached answer, so a command that
// finishes before the network does still gets it on a later run; it is shown
// at most once a day per release. The executable decides whether to check at
// all (Enabled) and prints the one line Finish returns.
//
// The cache is state.json next to preferences.toml. Unlike that file, which
// Coolship only reads, this one is Coolship's own and is written here.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultURL answers the newest release that is neither a draft nor a
	// pre-release.
	DefaultURL = "https://api.github.com/repos/joaomnuno/coolship/releases/latest"
	// EnvDisable, set to any value, turns the notifier off.
	EnvDisable = "COOLSHIP_NO_UPDATE_NOTIFIER"
	// StateFile is the cache's name in the preferences directory.
	StateFile = "state.json"
	// Interval is how long a check and a notice last.
	Interval = 24 * time.Hour
	// Timeout bounds one check, which never delays the command: Finish
	// abandons it when the command ends first.
	Timeout = 2 * time.Second
	// InstallCommand is the README's install line, which also upgrades.
	InstallCommand = "curl -fsSL https://raw.githubusercontent.com/joaomnuno/coolship/main/scripts/install.sh | sh"
)

// Conditions are the facts about one run that decide whether to check.
type Conditions struct {
	// Version is the running build's version, as --version reports it.
	Version string
	// StderrTerminal is whether stderr, where the notice goes, is a terminal.
	StderrTerminal bool
	// JSON is whether the run asked for --format json.
	JSON bool
	// Env reads the environment.
	Env func(string) string
	// Preference is the update_check preference; nil means not set.
	Preference *bool
}

// Enabled reports whether this run may check and notify: only for a person
// at a terminal (stderr a terminal, not CI, not JSON output), only for a
// released build, and only without an opt-out.
func Enabled(c Conditions) bool {
	env := c.Env
	if env == nil {
		env = func(string) string { return "" }
	}
	if !c.StderrTerminal || c.JSON || env("CI") != "" || env(EnvDisable) != "" {
		return false
	}
	if c.Preference != nil && !*c.Preference {
		return false
	}
	_, ok := parseVersion(c.Version)
	return ok
}

// State is the cache file's content.
type State struct {
	// CheckedAt is when GitHub last answered, and LatestVersion the release
	// it named.
	CheckedAt     time.Time `json:"checked_at,omitzero"`
	LatestVersion string    `json:"latest_version,omitempty"`
	// NotifiedAt is when a notice for NotifiedVersion was last shown.
	NotifiedAt      time.Time `json:"notified_at,omitzero"`
	NotifiedVersion string    `json:"notified_version,omitempty"`
}

// Notifier runs one check and produces at most one notice. Fields other than
// Current and StatePath have defaults.
type Notifier struct {
	Current   string
	StatePath string
	URL       string
	Client    *http.Client
	UserAgent string
	Now       func() time.Time

	cancel context.CancelFunc
	done   chan struct{}
}

// Start begins a check in the background when the last one is older than
// Interval. It returns immediately.
func (n *Notifier) Start(ctx context.Context) {
	state, _ := readState(n.StatePath)
	if fresh(n.now(), state.CheckedAt) {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	n.cancel = cancel
	n.done = make(chan struct{})
	go func() {
		defer close(n.done)
		defer cancel()
		n.check(ctx)
	}()
}

// Finish abandons a check still running and returns the notice to print, or
// "" when there is none. Call it once, after the command's own output. With
// show false, as for an interrupted run, nothing is returned and nothing is
// recorded as shown, so the notice is not lost for a day.
func (n *Notifier) Finish(show bool) string {
	if n.cancel != nil {
		n.cancel()
		<-n.done
	}
	if !show {
		return ""
	}
	state, err := readState(n.StatePath)
	if err != nil {
		return ""
	}
	current, ok := parseVersion(n.Current)
	latest, latestOK := parseVersion(state.LatestVersion)
	if !ok || !latestOK || compareVersions(latest, current) <= 0 {
		return ""
	}
	now := n.now()
	if state.NotifiedVersion == state.LatestVersion && fresh(now, state.NotifiedAt) {
		return ""
	}
	state.NotifiedAt, state.NotifiedVersion = now, state.LatestVersion
	// A cache that cannot be written only means the notice may repeat.
	_ = writeState(n.StatePath, state)
	return fmt.Sprintf("A new Coolship release is available: %s (you have %s). Upgrade: %s", state.LatestVersion, n.Current, InstallCommand)
}

// check asks GitHub and records the answer. A transport failure, including
// the check being abandoned, records nothing, so the next run tries again;
// any HTTP answer counts as a check, so a rate limit is not retried hourly.
func (n *Notifier) check(ctx context.Context) {
	latest, err := n.fetch(ctx)
	if err != nil {
		return
	}
	state, _ := readState(n.StatePath)
	state.CheckedAt = n.now()
	if latest != "" {
		state.LatestVersion = latest
	}
	_ = writeState(n.StatePath, state)
}

// fetch returns the latest release's tag, "" when the answer names none that
// counts, and an error only when no answer arrived.
func (n *Notifier) fetch(ctx context.Context) (string, error) {
	url := n.URL
	if url == "" {
		url = DefaultURL
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	if n.UserAgent != "" {
		request.Header.Set("User-Agent", n.UserAgent)
	}
	client := n.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", nil
	}
	var release struct {
		TagName    string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&release); err != nil {
		// A body cut short by the abandoned check is not an answer.
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", nil
	}
	parsed, ok := parseVersion(release.TagName)
	if release.Draft || release.Prerelease || !ok || len(parsed.pre) > 0 {
		return "", nil
	}
	return "v" + strings.TrimPrefix(release.TagName, "v"), nil
}

func (n *Notifier) now() time.Time {
	if n.Now != nil {
		return n.Now()
	}
	return time.Now()
}

// fresh reports whether at is within Interval before now; a time in the
// future, from a changed clock, is not.
func fresh(now, at time.Time) bool {
	age := now.Sub(at)
	return !at.IsZero() && age >= 0 && age < Interval
}

func readState(path string) (State, error) {
	var state State
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		// A damaged cache is replaced by the next write.
		return State{}, nil
	}
	return state, nil
}

// writeState replaces the file through a temporary file and a rename, so a
// concurrent run never reads half of it.
func writeState(path string, state State) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(dir, ".state-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), path)
}

// version is a semantic version: v0.3.0, or v0.3.1-rc.1 for a pre-release.
type version struct {
	core [3]int
	pre  []string
}

// parseVersion accepts a release or pre-release tag, with or without its v.
// A build Git describes past a tag (v0.3.0-5-gabc1234), a dirty tree, a Go
// pseudo-version, and dev builds are not releases and are refused, so they
// are never told to upgrade.
func parseVersion(text string) (version, bool) {
	var value version
	text = strings.TrimPrefix(text, "v")
	core, pre, hasPre := strings.Cut(text, "-")
	numbers := strings.Split(core, ".")
	if len(numbers) != 3 {
		return value, false
	}
	for i, number := range numbers {
		if !digits(number) {
			return value, false
		}
		parsed, err := strconv.Atoi(number)
		if err != nil {
			return value, false
		}
		value.core[i] = parsed
	}
	if !hasPre {
		return value, true
	}
	for identifier := range strings.SplitSeq(pre, ".") {
		if identifier == "" || identifier == "dirty" || strings.IndexFunc(identifier, func(r rune) bool {
			return !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z')
		}) >= 0 {
			return version{}, false
		}
		value.pre = append(value.pre, identifier)
	}
	return value, true
}

func digits(text string) bool {
	return text != "" && strings.IndexFunc(text, func(r rune) bool { return r < '0' || r > '9' }) < 0
}

// compareVersions orders by semantic versioning precedence.
func compareVersions(a, b version) int {
	for i := range a.core {
		if a.core[i] != b.core[i] {
			return compareInts(a.core[i], b.core[i])
		}
	}
	switch {
	case len(a.pre) == 0 && len(b.pre) == 0:
		return 0
	case len(a.pre) == 0:
		return 1
	case len(b.pre) == 0:
		return -1
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		x, y := a.pre[i], b.pre[i]
		if x == y {
			continue
		}
		xNumeric, yNumeric := digits(x), digits(y)
		switch {
		case xNumeric && yNumeric:
			xValue, _ := strconv.Atoi(x)
			yValue, _ := strconv.Atoi(y)
			return compareInts(xValue, yValue)
		case xNumeric:
			return -1
		case yNumeric:
			return 1
		default:
			return strings.Compare(x, y)
		}
	}
	return compareInts(len(a.pre), len(b.pre))
}

func compareInts(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
