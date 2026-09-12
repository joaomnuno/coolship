package ui

import (
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Verbosity is how much a run shows: normal, verbose, or debug.
type Verbosity int

const (
	// VerbosityNormal shows what the command prints and nothing more.
	VerbosityNormal Verbosity = iota
	// VerbosityVerbose adds one line per request with its status and time.
	VerbosityVerbose
	// VerbosityDebug adds each request and response in full, token masked.
	VerbosityDebug
)

// Exchange is one HTTP attempt, as the Coolify client reports it. Its fields
// match coolify.Exchange so the executable converts one into the other; ui
// does not import the client. The Authorization header arrives masked.
type Exchange struct {
	Method         string
	URL            string
	Attempt        int
	RequestHeader  http.Header
	RequestBody    []byte
	Status         int
	ResponseHeader http.Header
	ResponseBody   []byte
	Duration       time.Duration
	Err            error
}

// Trace is where verbose and debug output goes: stderr, next to the
// progress and diagnostics, so stdout and JSON stay what they are. The
// command tree sets the level once the flags are parsed; until then, and on a
// nil Trace, the level is normal and nothing is written. Live views are not
// drawn above normal, so request lines never break a spinner or a checklist.
type Trace struct {
	mu    sync.Mutex
	w     io.Writer
	level Verbosity
}

// NewTrace writes to w, which is the executable's stderr.
func NewTrace(w io.Writer) *Trace {
	if w == nil {
		w = io.Discard
	}
	return &Trace{w: w}
}

// SetLevel is called by the command tree once per run.
func (t *Trace) SetLevel(level Verbosity) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.level = level
}

// Level is the verbosity in effect; a nil Trace is normal.
func (t *Trace) Level() Verbosity {
	if t == nil {
		return VerbosityNormal
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.level
}

// Exchange writes one request: a single line when verbose, and the headers
// and bodies of both sides, curl-style, when debug.
func (t *Trace) Exchange(e Exchange) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.level == VerbosityNormal {
		return
	}
	var out strings.Builder
	out.WriteString(exchangeLine(e) + "\n")
	if t.level == VerbosityDebug {
		writeHeaders(&out, "> ", e.RequestHeader)
		writeBody(&out, "> ", e.RequestBody)
		if e.Status != 0 {
			writeHeaders(&out, "< ", e.ResponseHeader)
			writeBody(&out, "< ", e.ResponseBody)
		}
	}
	// Tracing is best effort; a lost diagnostic line must not fail a request.
	_, _ = io.WriteString(t.w, out.String())
}

// exchangeLine is the verbose line: method, URL, the status or the lack of
// one, the time taken, and which retry this was. A transport error is not
// repeated, since its text can carry more than the URL already shows.
func exchangeLine(e Exchange) string {
	result := "no response"
	if e.Status != 0 {
		result = fmt.Sprintf("%d %s", e.Status, http.StatusText(e.Status))
		result = strings.TrimSpace(result)
	}
	line := fmt.Sprintf("%s %s %s %s", singleLine(e.Method), singleLine(e.URL), result, e.Duration.Round(time.Millisecond))
	if e.Attempt > 0 {
		line += fmt.Sprintf(" (retry %d)", e.Attempt)
	}
	return line
}

func writeHeaders(out *strings.Builder, prefix string, header http.Header) {
	names := make([]string, 0, len(header))
	for name := range header {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		for _, value := range header[name] {
			out.WriteString(prefix + singleLine(name) + ": " + singleLine(value) + "\n")
		}
	}
}

// writeBody prints a body under the headers, one prefixed line per line,
// with control characters other than tabs dropped so a response cannot
// drive the terminal.
func writeBody(out *strings.Builder, prefix string, body []byte) {
	if len(body) == 0 {
		return
	}
	out.WriteString(strings.TrimSpace(prefix) + "\n")
	text := strings.TrimSuffix(string(body), "\n")
	for line := range strings.SplitSeq(text, "\n") {
		clean := strings.Map(func(r rune) rune {
			if r != '\t' && unicode.IsControl(r) {
				return -1
			}
			return r
		}, line)
		out.WriteString(prefix + clean + "\n")
	}
}
