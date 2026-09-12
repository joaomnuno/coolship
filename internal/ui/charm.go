package ui

// The Charm stack of ADR 0001 — the v2 modules at charm.land — is imported
// by internal/ui and nowhere else; see TestOnlyUIImportsTheCharmStack.
// Bubble Tea and Bubbles draw the spinner in wait.go, Lip Gloss renders the
// looks in style.go, and Huh draws the arrow-key selectors in select.go.
