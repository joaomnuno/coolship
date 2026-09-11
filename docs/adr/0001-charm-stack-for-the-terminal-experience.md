---
status: accepted
---

# Charm stack for the terminal experience

The brief asked for a polished terminal experience once the commands worked, and by v0.3 every command works with plain line output only. We chose Bubble Tea, Huh, and Lip Gloss for everything that runs against an attached terminal: spinners, the deployment stage checklist, arrow-key selection with filtering, and the `coolship ui` menu. Hand-rolling these on `x/term` was rejected because arrow keys, filtering, resize, and Windows terminals each carry their own edge cases, and the menu needs an event loop regardless.

We use the v2 modules at `charm.land`: `charm.land/bubbletea/v2`, `charm.land/bubbles/v2`, `charm.land/huh/v2`, and `charm.land/lipgloss/v2`, with `github.com/charmbracelet/colorprofile` carrying each stream's colour decision as an explicit profile. The v1 modules at `github.com/charmbracelet` were rejected because Bubble Tea v1's package init queries the terminal for its background colour and can stall every command for five seconds on a terminal that does not answer.

## Consequences

- `internal/ui` is the only package that imports the stack. The plain renderer for piped output and JSON stays as it is, so scripts and CI see no change.
- The upstream goal does not constrain this choice: only `config`, `project`, and `resolver` are candidates for Coolify CLI, and `internal/ui` is not.
- The module gains five direct and nineteen indirect dependencies (twenty-two indirect in total after `go mod tidy`). Swapping the stack later means rewriting every interactive view, so this decision is meant to hold.
