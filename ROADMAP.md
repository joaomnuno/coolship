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

This milestone shipped in v0.4.0 on 2026-09-16.

- [x] Adopt the Charm stack inside `internal/ui` only (ADR 0001).
- [x] `deploy`, `start`, `restart`, `preview`: a stage checklist keyed on Coolify's log markers, a spinner with elapsed time, the build log collapsed by default and printed in full on failure, and the application URL (or the Coolify deployment page) as the last line.
- [x] `--logs` and `--no-logs` for one run; then the preference; then the verbosity default (collapsed at normal, streamed at verbose and debug; the resolution lives in one place in `cmd`).
- [x] Selectors with arrow keys, type-to-filter, and Esc; normal mode shows names only, a UUID only to break a tie.
- [x] Verbosity: normal, `--verbose`, `--debug`, and `COOLSHIP_VERBOSITY` for CI; `-v` stays version (request lines and full exchanges on stderr).
- [x] Normal command output shows names only, and `--verbose` adds UUIDs (the rest of #18, tracked in #70; deployment timings above normal already print; the selectors item covers only the `link` and `init` choice).
- [x] Preferences at `~/.config/coolship/preferences.toml`; credentials stay in Coolify CLI's shared file (the file is read and shown by `config`; verbosity and build logs consume it, color not yet).
- [x] Help in five Cobra groups (Get started, Ship, Run, Configure, Maintain); every invocation stays flat.
- [x] `coolship ui`: a draft menu with the target, status, and last deployment above the verbs in the same groups (shipped as an experimental command; its keys and layout may change, and it may be removed).

## Milestone 4: secure credentials and stack services (v0.5)

This milestone does two things. Developers who want it can keep the Coolify token out of plaintext, and a project can declare the databases and services its application needs in `coolship.toml`, next to the application in the linked Coolify project. The existing rules still apply. The token is never a flag. Nothing is created until the developer has seen a plan. Nothing is deleted implicitly.

### Credentials in the OS keyring

Today every context's token sits in Coolify CLI's `~/.config/coolify/config.json` at mode 0600, and CI passes `COOLSHIP_URL` and `COOLSHIP_TOKEN` in the environment. `--debug` already masks the token and redacts secret fields. The file still leaves the token in plaintext at rest, where backups, dotfile sync, and accidental commits or copies take it along. A token exported in an interactive shell ends up in shell history and in `/proc/<pid>/environ` for every child process. A keyring would take the token off disk. It would not stop another process running as the same user while the keyring is unlocked, so same-user malware stays out of scope. ADR 0003 records the proposal below.

Peers differ. gh stores tokens in the keyring through `zalando/go-keyring` and falls back to a plain file, and that fallback is criticised for being silent ([cli/cli#10108](https://github.com/cli/cli/issues/10108)). Heroku uses the keychain with a `.netrc` fallback, and Doppler uses the keychain. Wrangler encrypts its credentials file with a key held in the OS keyring, behind an explicit `login --use-keyring` that can be turned off again. flyctl and Railway keep plain files.

- [ ] Write a threat model in `SECURITY.md` covering what Coolship protects (the token, secret values read from the server, deploy keys held in memory), the exposures it guards against (plaintext at rest, backups and dotfile sync, accidental commits or copies, CI logs, shell history), and what it does not attempt (other processes or malware running as the same user, a compromised account, or root).
- [ ] Store a context's token in the OS keyring through `zalando/go-keyring`. It is pure Go, so releases keep building with `CGO_ENABLED=0`. It uses `/usr/bin/security` on macOS, Secret Service over D-Bus on Linux, and Credential Manager on Windows, whose 2560-byte limit a Coolify token fits well within. `99designs/keyring` is rejected because it needs cgo on macOS and is no longer maintained.
- [ ] Make the keyring opt in with `login --use-keyring`, Wrangler's name for the same choice. The plain file stays the default because Coolify CLI reads only that file, and a context whose token lives in the keyring stops working in Coolify CLI. `login` says this before it writes.
- [ ] Keep keyring contexts in the shared file with a marker instead of the token, so names, URLs, and the default still live in one place.
- [ ] Resolve a token in one fixed order: the `COOLSHIP_URL` and `COOLSHIP_TOKEN` pair, then the keyring for a marked context, then the file.
- [ ] Never fall back silently. Headless Linux without Secret Service, WSL, and containers often have no keyring. There, `login --use-keyring` fails with the reason and names the plain-file alternative. If a marked context cannot be read from the keyring, that is an error, and Coolship does not look in the file instead.
- [ ] Make `logout` remove a context from both the keyring and the file.
- [ ] Have `doctor` report where each context's token lives and whether a keyring read works.
- [ ] Add a command that moves an existing context's token into the keyring and back, restoring the original when any step fails. Wrangler calls this `auth keyring enable` and `disable`; the name follows the peer survey in ADR 0002.
- [ ] Test against go-keyring's `MockInit` fake so that no test touches a real keyring.
- [ ] Stop showing `export COOLSHIP_TOKEN` for interactive shells in the documentation. The pair is meant for CI runners, which inject it as a masked secret.

### Services beside the application

On Coolify 4.3.x, `POST /api/v1/databases/{postgresql|mysql|mariadb|mongodb|redis|clickhouse|dragonfly|keydb}` creates a standalone database from `server_uuid`, `project_uuid`, and `environment`. `POST /api/v1/services` creates a one-click service from a type or a compose file, and each resource has status, start, stop, and restart endpoints. The create endpoints are not idempotent. `internal_db_url`, `external_db_url`, and passwords come back only for a token with sensitive read. Inside the server the host is the resource UUID, as in `postgres://user:pass@<uuid>:5432/db`. Coolify does not inject a standalone database's URL into an application. Standalone databases join the destination server's shared Docker network, while each one-click service gets its own network.

Peers take two approaches. Railway's `railway add` is imperative, and applications reference `${{Postgres.DATABASE_URL}}`. fly creates Postgres and Redis with separate explicit commands. Wrangler declares bindings in `wrangler.toml` with local variants for `dev`, and `render.yaml` declares databases that applications reference with `fromDatabase`. Docker Compose declares and runs a stack together. The plan is for Coolship to follow the declarative side, and ADR 0004 records the proposal:

```toml
[services.db]
type = "postgres"
version = "16"

[services.cache]
type = "redis"
version = "7"

[project.env]
DATABASE_URL = "{{services.db.url}}"
REDIS_URL = "{{services.cache.url}}"
```

- [ ] Declare services as `[services.<name>]` tables with a `type` and a `version`. Types map to Coolify's database endpoints first and to one-click services later.
- [ ] Treat the service name as the stable key. A service resolves by exact name inside the linked project and environment, with an optional UUID pin, as applications do.
- [ ] Resolve references such as `{{services.db.url}}` when services are applied or the application is deployed. The result becomes a Coolify variable on the application with the internal URL, and it never enters `coolship.toml` or a local file.
- [ ] Add `coolship services plan`, a diff-style report of what is missing, stopped, on a different version, or present remotely but no longer declared, with `--exit-code` as in `env diff`.
- [ ] Check for a service again right before creating it, and never replay a create request whose result is uncertain, as with deployments.
- [ ] Never delete a service because it was renamed or removed from the file. An explicit `services prune` names each service it will delete and asks first.
- [ ] Let every target of a monorepo in the same project and environment share services declared once at the top of the file.
- [ ] Keep public ports closed unless a service sets `public = true`.
- [ ] Report a token without sensitive read in `doctor` and in the plan, instead of writing an empty URL.
- [ ] Refuse a reference from an application on a different server from the service, because the shared network is per server. Document one-click service networking as a limit until it is verified.
- [ ] Settle when services are created (the open question below), then build that path.
- [ ] Verify against the isolated test project on the live instance and record the observed behavior in `ARCHITECTURE.md` section 10.

Backups and restores of declared databases are out of scope for this milestone.

### Open question: when services are created

Creating a database takes time, can open a public port, and adds a resource that someone has to pay for and back up. `ARCHITECTURE.md` also says deployment never silently creates resources. There are three options:

- `init` creates the declared services along with the application. Setup happens in one step, but a service added to the file later still needs another path.
- `deploy` creates whatever is missing before it deploys. Nothing has to be remembered, but a deployment that looks routine can create resources and take far longer than usual, which is the silent creation the architecture rules out.
- The explicit `coolship services plan` and `coolship services apply` commands create services. `deploy` checks that the declared services exist and are running before it deploys. When they do not, an interactive run shows the plan and offers to create them. A non-interactive run fails with the plan unless `--yes` is given.

The recommendation is the third option. It is the only one in which a developer sees a plan before anything is created and can still create services in a single step. `init` can offer to run `services apply` when the file already declares services.

## Milestone 5: the stack on your machine (v0.6)

Today `coolship dev` runs a local process with the target's remote Coolify variables injected (`cmd/dev.go`, `internal/service/dev.go`). This milestone runs the declared services, and optionally the application, on the developer's machine in Docker. The goal is similar to what Miniflare does for Wrangler. The approach follows `supabase start` and `encore run`: Coolship would start real images and not emulate anything in its own process. It would drive the `docker compose` command through an injected runner, as it drives `git` today, so the module would gain no Docker SDK.

- [ ] Generate a compose file into Coolship's cache directory from the `[services.*]` tables, using pinned official images for the declared versions, and start it with `docker compose up --wait`.
- [ ] Keep today's behavior for a file without `[services.*]` tables. With services declared, the local stack becomes the default, and `dev --remote` keeps the current behavior.
- [ ] Inject localhost URLs under the variable names the references produce remotely. Every other variable comes from Coolify as it does today.
- [ ] Bind ports to 127.0.0.1 on ports chosen and remembered per project, so two projects can run at the same time.
- [ ] Keep data in named volumes per project. `dev down` stops the stack and keeps the data, and `dev reset` removes the volumes after confirmation.
- [ ] Seed a fresh database from a declared SQL file or command on first start and after a reset.
- [ ] Optionally run the application itself in a container built from its build pack: Dockerfile and Compose directly, and Nixpacks or Railpack through their own command-line tools when installed. When a tool is missing, Coolship refuses and says how to install it.
- [ ] Report in `doctor` whether Docker and Compose v2 are available. When they are not, `dev` fails with the reason and does not quietly fall back to remote variables.
- [ ] Document the gaps between local and remote. Coolify's `SERVICE_FQDN_*` and `SERVICE_URL_*` variables, proxy labels, networks, and health checks do not exist locally, so the local stack is a development environment and not a copy of the server.

## Later

- [x] `init`: create a Coolify application from a repository's remote and link it — public repositories, and private ones through a GitHub App or a deploy key, including generating the key (all three verified live on 4.3.18), with every build pack Coolify offers: Railpack, Nixpacks, static, Dockerfile, and Compose with per-service domains.
- [x] Lifecycle and history: `stop`, `start`, `restart`, `deployments`, and `cancel`, with `status` showing the last deployment (verified live on 4.3.18).
- [ ] Propose project-local commands to Coolify CLI using `config`, `project`, and `resolver` verbatim.
- [x] Report upstream: the preview twin of a shown-once variable is returned in clear on 4.3.18 (reported privately to Coolify on 2026-09-10, per their security policy).
- [ ] Revisit `logs --follow` if a server cursor or streaming endpoint appears.

## Toward 1.0

These are ideas for where Coolship could go, not commitments. None of them has a milestone yet, and some depend on Coolify adding endpoints it does not have today. Each one keeps Coolship a tool you run inside a project and does not turn it into an administration client.

### A contract others can build on

- Stable, documented JSON output and exit codes for every command, promised under Semantic Versioning from 1.0 on.
- The project-local commands upstreamed into Coolify CLI, or a public Go module both tools share, once `config`, `project`, and `resolver` have stopped changing.
- A GitHub Action and CI templates that install a pinned release, run `services plan` and `deploy`, and post the application URL.
- An MCP server that exposes status, deployments, and logs to AI assistants, and asks the developer to confirm before anything is deployed or changed. The documentation site already publishes `llms.txt`.

### Environments that come and go

- A preview environment for each pull request, with its own services and seeded data, torn down when the pull request is merged or closed. The API has no endpoint that creates previews yet, so this needs Coolify's webhook or a new endpoint.
- One-command rollback to a previous deployment, shown in `deployments`.
- Promotion of the same commit from staging to production, across servers or across Coolify instances, with each step's plan shown before it runs.
- `coolship init --template` to start a new project from a template that already declares an application and its services.

### Seeing and keeping what runs

- `coolship ui` grown from a menu into a live view of logs, deployments, and rollbacks. This would change the menu's definition in `CONTEXT.md`.
- Backups and restores for declared databases, built on Coolify's scheduled backups.
- A view of resource usage for the target and its services.
- Secrets pulled from 1Password, Doppler, or SOPS into Coolify variables, following the same scope and prune rules as `env push`.

### Trust and reach

- Signed releases with an SBOM, next to the checksums that are published today.
- Packages for Homebrew, Scoop, apt, and Nix alongside `scripts/install.sh`.
- Windows treated as a first-class platform, with the end-to-end suite running on it.
- No telemetry. If it is ever added, it is opt-in and documented.

## Scope

Coolship works inside one linked Coolify project. The plan for v0.5 is to create the databases and services a project declares, next to the application in that project and environment, and only after showing a plan. The plan for v0.6 is to run that stack locally in Docker using real images, without emulating anything in its own process. Server administration, proxy orchestration, and automatic GitHub integration stay out of scope.
