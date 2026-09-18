package ui

import (
	"fmt"
	"io"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Block is the thick bar down the left of one command's run, the one Huh
// draws beside a focused field, so a run that asks, works, and reports reads
// as one piece: init and link draw their plan, confirmation, checklist,
// result line, and next steps inside it, and the pickers between them carry
// the same bar already. A command opts in by setting Streams.Block; every
// writer in this package that prints for that run then goes through it.
//
// A nil Block is a run without a bar and writes text exactly as given, which
// is every run off a terminal, with JSON output, or above normal verbosity.
// The bar is a character like any other: without colour it stays, unstyled.
type Block struct {
	bar   string     // the styled glyph and the padding after it
	cells int        // columns bar takes
	width func() int // the terminal's columns now; 0 when unknown
}

// NewBlock is the bar for a run on these streams, or nil when there is none:
// the same terms a live view is drawn on, human output on an interactive
// stderr terminal at normal verbosity.
func NewBlock(streams Streams, format string) *Block {
	if format != "human" {
		return nil
	}
	streams = streams.Normalized()
	terminal, ok := drawable(streams)
	if !ok {
		return nil
	}
	return newBlock(streams.errPalette(), func() int { return terminalWidth(terminal) })
}

// newBlock takes the bar from the theme the pickers use, so the glyph, its
// colour, and the padding after it are Huh's own and stay so.
func newBlock(style palette, width func() int) *Block {
	base := pickerTheme(style).Theme(true).Focused.Base
	glyph := base.GetBorderStyle().Left
	bar := style.render(lipgloss.NewStyle().Foreground(base.GetBorderLeftForeground()), glyph) +
		strings.Repeat(" ", base.GetPaddingLeft())
	return &Block{bar: bar, cells: ansi.StringWidth(bar), width: width}
}

// Active reports whether a bar is drawn.
func (b *Block) Active() bool { return b != nil }

// prompt puts the bar before a line that is left open for an answer.
func (b *Block) prompt(text string) string {
	if b == nil {
		return text
	}
	return b.bar + text
}

// rows puts the bar before each row of a live view and cuts every row to
// width, so a frame stays one terminal line per row and the bar unbroken.
func (b *Block) rows(lines []string, width int) string {
	if b != nil {
		for i, line := range lines {
			lines[i] = b.bar + line
		}
	}
	return fitLines(lines, width)
}

// lines wraps text, which may be styled and hold several lines, to what is
// left of the terminal beside the bar, and puts the bar before every line,
// so a long value continues inside the block instead of breaking the bar.
func (b *Block) lines(text string) []string {
	lines := strings.Split(text, "\n")
	if b == nil {
		return lines
	}
	room := 0
	if width := b.width(); width > b.cells+minBlockRoom {
		room = width - b.cells
	}
	var out []string
	for _, line := range lines {
		if room > 0 {
			line = lipgloss.Wrap(line, room, "")
		}
		for _, part := range strings.Split(line, "\n") {
			out = append(out, b.bar+part)
		}
	}
	return out
}

// hanging is lines for a row made of a lead, such as a key and the padding to
// its column, and a value: a value too long for the terminal continues under
// itself, so the column holds on a narrow terminal. When the lead leaves no
// room worth wrapping to, the row wraps as any other line.
func (b *Block) hanging(lead, value string) []string {
	if b == nil {
		return []string{lead + value}
	}
	indent := ansi.StringWidth(lead)
	room := b.width() - b.cells - indent
	if room < minBlockRoom {
		return b.lines(lead + value)
	}
	parts := strings.Split(lipgloss.Wrap(value, room, ""), "\n")
	for i, part := range parts {
		if i == 0 {
			parts[i] = b.bar + lead + part
		} else {
			parts[i] = b.bar + strings.Repeat(" ", indent) + part
		}
	}
	return parts
}

// minBlockRoom is the narrowest text column worth wrapping to; a terminal
// narrower than that wraps by itself, as it would without a bar.
const minBlockRoom = 10

// println writes text and a newline, inside the bar when there is one.
func (b *Block) println(w io.Writer, text string) error {
	_, err := fmt.Fprintln(w, strings.Join(b.lines(text), "\n"))
	return err
}
