package ui

// The Charm stack of ADR 0001 is imported here and nowhere else; see
// TestOnlyUIImportsTheCharmStack. Bubble Tea and Bubbles draw the spinner in
// wait.go and Lip Gloss renders the looks in style.go. Huh, the form library
// the arrow-key selectors of milestone 3 build on, is pinned in go.mod through
// this import until those selectors land.
//
// Known cost of the v1 stack: Bubble Tea's package init asks Lip Gloss's
// default renderer for the terminal's background colour, which writes an OSC
// 11 query and a cursor position request to stdout when stdout is a terminal
// (never in CI, never when piped) and waits up to termenv.OSCTimeout for the
// answer. Nothing here reads that answer; the palettes in style.go take the
// colour decision from Streams. The query is gone in Bubble Tea v2.
import (
	_ "github.com/charmbracelet/huh"
)
