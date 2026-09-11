---
status: accepted
---

# Charm stack for the terminal experience

The brief asked for a polished terminal experience once the commands worked, and by v0.3 every command works with plain line output only. We chose Bubble Tea, Huh, and Lip Gloss for everything that runs against an attached terminal: spinners, the deployment stage checklist, arrow-key selection with filtering, and the `coolship ui` menu. Hand-rolling these on `x/term` was rejected because arrow keys, filtering, resize, and Windows terminals each carry their own edge cases, and the menu needs an event loop regardless.

## Consequences

- `internal/ui` is the only package that imports the stack. The plain renderer for piped output and JSON stays as it is, so scripts and CI see no change.
- The upstream goal does not constrain this choice: only `config`, `project`, and `resolver` are candidates for Coolify CLI, and `internal/ui` is not.
- The module gains roughly fifteen indirect dependencies. Swapping the stack later means rewriting every interactive view, so this decision is meant to hold.
