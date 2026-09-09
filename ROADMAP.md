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

## Later milestones

- [x] Validate against a live Coolify instance and record a supported server baseline (4.3.18).
- [ ] Add `open`, `unlink`, `config`, and `doctor` where useful.
- [ ] Add environment pull, diff, and push with explicit scope and secret handling.
- [ ] Add named monorepo targets and configuration migration rules.
- [ ] Add local process execution for `dev` without a bespoke runtime.
- [ ] Define preview deployment support against verified server capabilities.
- [ ] Consider shared packages with Coolify CLI after interfaces stabilize.

No server administration, runtime emulation, proxy orchestration, or automatic GitHub integration is part of the first milestone.
