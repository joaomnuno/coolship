// Package ui owns terminal presentation and interaction for Coolship workflows.
package ui

import (
	"io"
	"strings"
)

// Streams contains explicitly supplied process streams and terminal capability.
// The executable determines Interactive from stdin, stderr, and CI settings.
type Streams struct {
	In          io.Reader
	Out         io.Writer
	Err         io.Writer
	Interactive bool
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
