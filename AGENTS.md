# Working on Coolship

Read `ARCHITECTURE.md` and `ROADMAP.md` before substantial changes. `REQUEST.md` records the original product brief.

## Scope and structure

- Keep implementation, fixtures, auxiliary tools, and integration infrastructure inside this repository. Reference sibling Coolify and Workers SDK repositories without changing them unless the task requires it.
- Coolship is a project-local developer CLI, not a general Coolify administration client.
- Use one Go module, `github.com/joaomnuno/coolship`, with the executable at the root and Cobra constructors in `cmd/`.
- Keep discovery and local writes in `internal/project`, the TOML schema in `internal/config`, credentials in `internal/auth`, and HTTP in `internal/coolify`.
- Commands call `internal/service`; all workflows share project preparation and `internal/resolver`. UI code owns presentation and prompts.
- Keep consumer-owned interfaces small. Pass `context.Context` explicitly; do not use global configuration, clients, streams, or working-directory changes.
- Never import sibling modules' `internal` packages. Keep auxiliary tools under this monorepo.

## Go conventions and validation

- Use `./scripts/go` for Go commands. It keeps caches and temporary build/test files inside the repository.
- Format changed Go files with `gofmt`. Wrap errors with `%w` when callers need the cause; use `errors.Is` and `errors.As`.
- Keep HTTP requests cancellable. Never automatically replay a deployment POST after an uncertain response.
- Return typed data and errors from lower layers. Print and classify errors once at the executable boundary.
- Use temporary directories, injected writers/clocks, and `httptest.Server` for behavioral tests. Do not require actual user credentials for tests.
- Run focused tests while developing, then `./scripts/go test ./...`, `./scripts/go test -race ./...`, and `./scripts/go vet ./...` for the milestone.
- Keep credential files read-only. Never persist tokens or fetched secret values in project configuration, fixtures, debug output, or Git.
- Update README examples, architecture decisions, and roadmap status when behavior changes. Do not document planned commands as implemented.

## Parallel work

When the user authorizes agents, delegate bounded packages with explicit ownership and agree on interfaces first. All agents share the working tree. Do not overwrite another agent's work. The coordinating agent handles integration, final checks, commits, and pushes.

## References

- [Go module organization](https://go.dev/doc/modules/layout)
- [Context propagation](https://go.dev/blog/context)
- [Wrapping and inspecting errors](https://go.dev/blog/go1.13-errors)
- [HTTP test servers](https://pkg.go.dev/net/http/httptest)
