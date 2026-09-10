package ui

// palette applies ANSI SGR styling to sanitized text, or nothing at all. Each
// stream carries its own palette because stdout may be piped while stderr is
// still a terminal. Callers pass text through singleLine before styling, so a
// style never wraps raw input.
type palette struct {
	enabled bool
}

// SGR parameter lists. Compound styles keep to one sequence per wrapped text.
const (
	bold      = "1"
	dim       = "2"
	red       = "31"
	green     = "32"
	yellow    = "33"
	cyan      = "36"
	redBold   = "1;31"
	greenBold = "1;32"
)

// apply wraps text in the given SGR style. Empty text and disabled palettes
// return text unchanged, so styled output equals plain output byte for byte
// whenever color is off.
func (p palette) apply(style, text string) string {
	if !p.enabled || style == "" || text == "" {
		return text
	}
	return "\x1b[" + style + "m" + text + "\x1b[0m"
}

// key styles a "Label:" prefix of a key/value line; values stay plain.
func (p palette) key(label string) string {
	return p.apply(dim, label+":")
}

// deploymentStatus styles the server's deployment status words. Unknown
// statuses stay plain rather than guessing at their meaning.
func deploymentStatus(status string) string {
	switch status {
	case "queued":
		return dim
	case "in_progress":
		return cyan
	case "finished":
		return greenBold
	case "failed", "cancelled-by-user":
		return redBold
	}
	return ""
}
