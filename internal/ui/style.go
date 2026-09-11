package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
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
// The stream decides whether colour is on; the palette turns that decision
// into an explicit colour profile — ANSI when on, NoTTY when off — and passes
// everything Lip Gloss renders through a colorprofile.Writer with that
// profile, so nothing here inspects the environment or the terminal, and with
// colour off every look renders its text unchanged, byte for byte. Callers
// pass text through singleLine before styling, so a style never wraps raw
// input.
type palette struct {
	profile colorprofile.Profile
	styles  [looks]lipgloss.Style
}

// newPalette builds the looks for the given colour decision.
func newPalette(color bool) palette {
	p := palette{profile: colorprofile.NoTTY}
	if color {
		p.profile = colorprofile.ANSI
	}
	// Every look keeps tabs as they are; Lip Gloss would otherwise expand
	// them, and text is the caller's to shape.
	base := lipgloss.NewStyle().TabWidth(lipgloss.NoTabConversion)
	p.styles[plain] = base
	p.styles[bold] = base.Bold(true)
	p.styles[dim] = base.Faint(true)
	p.styles[red] = base.Foreground(lipgloss.Red)
	p.styles[green] = base.Foreground(lipgloss.Green)
	p.styles[yellow] = base.Foreground(lipgloss.Yellow)
	p.styles[cyan] = base.Foreground(lipgloss.Cyan)
	p.styles[redBold] = base.Bold(true).Foreground(lipgloss.Red)
	p.styles[greenBold] = base.Bold(true).Foreground(lipgloss.Green)
	return p
}

// apply renders text in the given look and downsamples the result to the
// stream's profile. Empty text and the plain look return text unchanged, so
// nothing is ever wrapped for nothing.
func (p palette) apply(l look, text string) string {
	if l == plain || text == "" {
		return text
	}
	var rendered strings.Builder
	writer := colorprofile.Writer{Forward: &rendered, Profile: p.profile}
	// A strings.Builder never fails to write.
	_, _ = writer.WriteString(p.styles[l].Render(text))
	return rendered.String()
}

// key styles a "Label:" prefix of a key/value line; values stay plain.
func (p palette) key(label string) string {
	return p.apply(dim, label+":")
}

// colorProfile is the profile a Bubble Tea program on this stream renders
// with, so the program never detects one. A terminal without colour is still
// a terminal, hence ASCII rather than NoTTY there.
func (p palette) colorProfile() colorprofile.Profile {
	if p.profile == colorprofile.ANSI {
		return colorprofile.ANSI
	}
	return colorprofile.ASCII
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
