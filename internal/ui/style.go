package ui

import (
	"io"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// look names one of the styles a palette can apply. plain applies nothing.
type look int

const (
	plain look = iota
	bold
	dim
	red
	green
	yellow
	cyan
	redBold
	greenBold
	looks
)

// palette renders looks with Lip Gloss for one stream. Each stream carries its
// own palette because stdout may be piped while stderr is still a terminal.
// The stream decides whether colour is on; the palette's renderer gets that
// decision as an explicit colour profile — ANSI when on, Ascii when off — so
// Lip Gloss never probes the terminal itself, and with colour off every look
// renders its text unchanged, byte for byte. Callers pass text through
// singleLine before styling, so a style never wraps raw input.
type palette struct {
	styles [looks]lipgloss.Style
}

// newPalette builds the looks on a renderer bound to w with the given colour
// decision. The renderer is created with its profile, so neither termenv nor
// Lip Gloss inspects the environment or the writer.
func newPalette(w io.Writer, color bool) palette {
	profile := termenv.Ascii
	if color {
		profile = termenv.ANSI
	}
	renderer := lipgloss.NewRenderer(w, termenv.WithProfile(profile))
	renderer.SetColorProfile(profile)
	// Every look keeps tabs as they are; Lip Gloss would otherwise expand
	// them, and text is the caller's to shape.
	base := renderer.NewStyle().TabWidth(lipgloss.NoTabConversion)
	var p palette
	p.styles[plain] = base
	p.styles[bold] = base.Bold(true)
	p.styles[dim] = base.Faint(true)
	p.styles[red] = base.Foreground(lipgloss.Color("1"))
	p.styles[green] = base.Foreground(lipgloss.Color("2"))
	p.styles[yellow] = base.Foreground(lipgloss.Color("3"))
	p.styles[cyan] = base.Foreground(lipgloss.Color("6"))
	p.styles[redBold] = base.Bold(true).Foreground(lipgloss.Color("1"))
	p.styles[greenBold] = base.Bold(true).Foreground(lipgloss.Color("2"))
	return p
}

// apply renders text in the given look. Empty text and the plain look return
// text unchanged, so nothing is ever wrapped for nothing.
func (p palette) apply(l look, text string) string {
	if l == plain || text == "" {
		return text
	}
	return p.styles[l].Render(text)
}

// key styles a "Label:" prefix of a key/value line; values stay plain.
func (p palette) key(label string) string {
	return p.apply(dim, label+":")
}

// style returns the Lip Gloss style of a look, for views that render
// themselves, such as the spinner.
func (p palette) style(l look) lipgloss.Style {
	return p.styles[l]
}

// deploymentStatus styles the server's deployment status words. Unknown
// statuses stay plain rather than guessing at their meaning.
func deploymentStatus(status string) look {
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
	return plain
}
