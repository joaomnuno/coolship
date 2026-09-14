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
// see escape sequences; JSON output is never styled regardless. Trace is the
// verbosity sink on Err, shared by every copy of the streams; nil is normal.
//
// OutTerminal and ErrTerminal say whether each stream is a terminal, and
// Width is stdout's width in columns when it is one (zero when unknown).
// Human output shapes itself for a person only on a terminal: names without
// UUIDs at normal verbosity, relative times, status glyphs, and a table that
// fits. A pipe gets the text it always did, so scripts reading it keep
// working; the zero value is a pipe.
type Streams struct {
	In          io.Reader
	Out         io.Writer
	Err         io.Writer
	Interactive bool
	ColorOut    bool
	ColorErr    bool
	OutTerminal bool
	ErrTerminal bool
	Width       int
	Trace       *Trace
}

// level is the verbosity in effect for these streams.
func (s Streams) level() Verbosity { return s.Trace.Level() }

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

func (s Streams) outPalette() palette { return newPalette(s.ColorOut) }
func (s Streams) errPalette() palette { return newPalette(s.ColorErr) }
