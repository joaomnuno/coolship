# Coolship roadmap

The first milestone validates the architecture through `link`, `status`, `deploy`, and `logs`. Auxiliary tools and integration infrastructure remain inside this monorepo.

## Milestone 0: architecture

- [x] Inspect Coolify CLI, Wrangler, and Coolify server source.
- [x] Document packages, dependency direction, project context, and compatibility constraints.
- [x] Commit and push the initial architecture and engineering brief.

## Milestone 1: working project workflows

- [x] Add a Go module, explicit dependency wiring, and repository development guidance.
- [x] Implement TOML configuration, bounded discovery, and safe binding writes.
- [x] Reuse Coolify CLI contexts and support explicit CI credentials.
- [x] Implement scoped resource resolution and a minimal cancellable HTTP adapter.
- [x] Implement interactive and noninteractive `link`.
- [x] Implement `status`, deployment observation, and runtime log following.
- [x] Verify CLI flows against a controlled HTTP server and document server compatibility limits.
- [x] Run tests, race detection, vet, build, and command smoke checks.
- [x] Update README and architecture to reflect implemented behavior; commit and push the milestone.

At this milestone `link`, `status`, `deploy`, and `logs` worked end to end against a controlled server only. Milestone 2 then verified them, and every later command, against a live Coolify 4.3.18 instance; README and `ARCHITECTURE.md` section 10 record the supported baseline and its limits.

## Milestone 2: the rest of the brief, verified live

Every command from the brief is implemented and verified against a live Coolify 4.3.18 instance, using a purpose-built public example repository and an isolated project on that instance.

- [x] Validate against a live Coolify instance and record a supported server baseline (4.3.18).
- [x] Fix what live validation found: log follow across snapshots without a final newline, interrupt reporting.
- [x] Stream build logs during deployment observation.
- [x] Add `open`, `unlink`, `config`, and `doctor`.
- [x] Add environment pull, diff, and push with explicit scope and secret handling.
- [x] Define and verify preview deployment support against server capabilities.
- [x] Add named monorepo targets and configuration migration rules.
- [x] Add local process execution for `dev` without a bespoke runtime.
- [x] Decide on shared packages with Coolify CLI (no shared package yet; the seam is documented in `ARCHITECTURE.md`).

## Milestone 3: the terminal experience (v0.4)

Settled on 2026-09-11; `CONTEXT.md` holds the vocabulary and ADR 0001 the library choice. v0.3 ships with the plain output it has; this milestone is what a developer sees when a terminal is attached. Piped output and JSON do not change.

- [x] Adopt the Charm stack inside `internal/ui` only (ADR 0001).
- [x] `deploy`, `start`, `restart`, `preview`: a stage checklist keyed on Coolify's log markers, a spinner with elapsed time, the build log collapsed by default and printed in full on failure, and the application URL (or the Coolify deployment page) as the last line.
- [x] `--logs` and `--no-logs` for one run; then the preference; then the verbosity default (collapsed until verbosity lands; the resolution lives in one place in `cmd`).
- [ ] Selectors with arrow keys, type-to-filter, and Esc; normal mode shows names only, a UUID only to break a tie.
- [ ] Verbosity: normal, `--verbose`, `--debug`, and `COOLSHIP_VERBOSITY` for CI; `-v` stays version.
- [x] Preferences at `~/.config/coolship/preferences.toml`; credentials stay in Coolify CLI's shared file (the file is read and shown by `config`; verbosity and build logs consume it once they land).
- [x] Help in five Cobra groups (Get started, Ship, Run, Configure, Maintain); every invocation stays flat.
- [ ] `coolship ui`: a draft menu with the target, status, and last deployment above the verbs in the same groups.

## Later

- [x] `init`: create a Coolify application from a repository's remote and link it — public repositories, and private ones through a GitHub App or a deploy key, including generating the key (all three verified live on 4.3.18), with every build pack Coolify offers: Railpack, Nixpacks, static, Dockerfile, and Compose with per-service domains.
- [x] Lifecycle and history: `stop`, `start`, `restart`, `deployments`, and `cancel`, with `status` showing the last deployment (verified live on 4.3.18).
- [ ] Propose project-local commands to Coolify CLI using `config`, `project`, and `resolver` verbatim.
- [x] Report upstream: the preview twin of a shown-once variable is returned in clear on 4.3.18 (reported privately to Coolify on 2026-09-10, per their security policy).
- [ ] Revisit `logs --follow` if a server cursor or streaming endpoint appears.

No server administration, runtime emulation, proxy orchestration, or automatic GitHub integration is in scope.
