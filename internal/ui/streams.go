// Package ui owns terminal presentation and interaction for Coolship workflows.
package ui

import (
	"io"
	"strings"
)

// Streams contains explicitly supplied process streams and terminal capability.
// The executable determines Interactive from stdin, stderr, and CI settings,
// and ColorOut/ColorErr per stream from terminal detection, NO_COLOR, TERM,
// CI, and --no-color. Zero values render plain text, so tests and pipes never
// see escape sequences; JSON output is never styled regardless.
type Streams struct {
	In          io.Reader
	Out         io.Writer
	Err         io.Writer
	Interactive bool
	ColorOut    bool
	ColorErr    bool
}

// Normalized supplies inert streams where the caller omitted them.
func (s Streams) Normalized() Streams {
	if s.In == nil {
		s.In = strings.NewReader("")
	}
	if s.Out == nil {
		s.Out = io.Discard
	}
	if s.Err == nil {
		s.Err = io.Discard
	}
	return s
}

func (s Streams) outPalette() palette { return palette{enabled: s.ColorOut} }
func (s Streams) errPalette() palette { return palette{enabled: s.ColorErr} }
