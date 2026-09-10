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
- Keep the `go` directive equal to Coolify CLI's, currently `go 1.26`, because its CI derives the Go version from `go.mod` and upstreamed code cannot require a newer floor. Develop with any newer toolchain; raise the directive only when Coolify CLI raises its own, or when a language or standard library feature genuinely requires it.
- Format changed Go files with `gofmt`. Wrap errors with `%w` when callers need the cause; use `errors.Is` and `errors.As`.
- Keep HTTP requests cancellable. Never automatically replay a deployment POST after an uncertain response.
- Return typed data and errors from lower layers. Print and classify errors once at the executable boundary.
- Use temporary directories, injected writers/clocks, and `httptest.Server` for behavioral tests. Do not require actual user credentials for tests.
- Run focused tests while developing, then `./scripts/go test ./...`, `./scripts/go test -race ./...`, and `./scripts/go vet ./...` for the milestone. CI (`.github/workflows/ci.yml`) runs the same plus `gofmt -l`, `go build ./...`, and `shellcheck` on `scripts/`; a pull request that changes Go code without a `CHANGELOG.md` entry fails unless it carries the `skip-changelog` label.
- Keep credential files read-only. Never persist tokens or fetched secret values in project configuration, fixtures, debug output, or Git.
- Update README examples, architecture decisions, and roadmap status when behavior changes. Do not document planned commands as implemented.

## Versioning and changelog

- Versions are Git tags `vMAJOR.MINOR.PATCH` following Semantic Versioning; before 1.0.0 a minor version may change command behavior. `CHANGELOG.md` follows Keep a Changelog.
- Every user-visible change lands with an entry under `## [Unreleased]` in the same commit, in the past tense from the user's point of view (Added, Changed, Fixed, Removed, Compatibility). Internal refactors need no entry.
- To release: move `[Unreleased]` into a new `## [X.Y.Z] - YYYY-MM-DD` section, update the link references at the bottom, commit as `release: vX.Y.Z`, tag with `git tag -a vX.Y.Z -m "Coolship vX.Y.Z"`, and push the tag. Pushing the tag runs the Release workflow (`.github/workflows/release.yml`), which builds every platform with GoReleaser (`.goreleaser.yaml`) and publishes the GitHub Release with notes from that changelog section (`scripts/release-notes`); a missing section fails the release. Tags with a pre-release suffix such as `vX.Y.Z-rc.1` are published as pre-releases with notes from `[Unreleased]` and never become `latest`. Run the Release workflow manually with a `tag` input to republish an existing tag, and the Nightly workflow to refresh the rolling `nightly` pre-release from `main`. Locally, `scripts/build` stamps the version from `git describe`; a plain `go build` reports the VCS revision instead.
- The archive and checksum names in `.goreleaser.yaml` are a contract with `scripts/install.sh`; change them together.
- Bump the `Compatibility` note when the verified Coolify version changes.

## Parallel work

When the user authorizes agents, delegate bounded packages with explicit ownership and agree on interfaces first. All agents share the working tree. Do not overwrite another agent's work. The coordinating agent handles integration, final checks, commits, and pushes.

## References

- [Go module organization](https://go.dev/doc/modules/layout)
- [Context propagation](https://go.dev/blog/context)
- [Wrapping and inspecting errors](https://go.dev/blog/go1.13-errors)
- [HTTP test servers](https://pkg.go.dev/net/http/httptest)
