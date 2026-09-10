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

`link`, `status`, `deploy`, and `logs` work end to end against a controlled server. No live Coolify instance has been contacted, so there is no verified supported server version; README and `ARCHITECTURE.md` section 10 record the limits that follow.

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

## Later

- [x] `init`: create a Coolify application from a repository's public remote and link it (verified live on 4.3.18). Private repositories need a GitHub App or deploy key the API takes by UUID, and Compose projects need per-service domains; both remain open.
- [ ] Propose project-local commands to Coolify CLI using `config`, `project`, and `resolver` verbatim.
- [x] Report upstream: the preview twin of a shown-once variable is returned in clear on 4.3.18 (reported privately to Coolify on 2026-09-10, per their security policy).
- [ ] Revisit `logs --follow` if a server cursor or streaming endpoint appears.

No server administration, runtime emulation, proxy orchestration, or automatic GitHub integration is in scope.
