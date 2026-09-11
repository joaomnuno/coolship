package ui

// The Charm stack of ADR 0001 — the v2 modules at charm.land — is imported
// here and nowhere else; see TestOnlyUIImportsTheCharmStack. Bubble Tea and
// Bubbles draw the spinner in wait.go and Lip Gloss renders the looks in
// style.go. Huh, the form library the arrow-key selectors of milestone 3
// build on, is pinned in go.mod through this import until those selectors
// land.
import (
	_ "charm.land/huh/v2"
)
