# Changelog

All notable changes to Coolship are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/). Before 1.0.0, minor versions may
change command behavior; the changelog says when they do.

## [Unreleased]

### Added

- `stop`, `start`, `restart`: manage the linked application's lifecycle. `stop` shows the application and its environment, asks (or takes `--yes`), then polls the status until it reports `exited` or `--timeout` (default 2m) passes, and reports the last status seen; Coolify stops and removes the containers, and `deploy` or `start` brings them back. Only an application that already reports `exited` is left alone, with a warning; any other status is stopped, as in Coolify's own UI, so a crash loop shown as `restarting` or `degraded` can be stopped. `start` and `restart` queue a deployment through Coolify's start and restart actions and observe it exactly like `deploy`, build log included, with `--no-wait` and `--timeout`; `restart` asks first or takes `--yes`. Restart-only deployments skip the build when the commit's image exists, except for Dockerfile and Docker image applications, which Coolify deploys in full.
- `deployments`: list the linked application's most recent deployments (`-n`, default 10), newest first, with a short UUID, status, commit, type (deploy, restart, rollback, or a pull request preview), creation time, and duration; `--format json` carries the full UUIDs, source (api, webhook, manual), and timestamps. Build logs are never included.
- `cancel`: cancel a queued or in-progress deployment — the one named, checked to belong to the linked application, or the single active one, refusing when there is none or more than one. Asks first or takes `--yes`; deployments that already ended are refused before any request.
- `status` now adds a `Last deployment` line (short UUID, status, commit, time) and a `last_deployment` JSON object from the deployment history when it can be read; a history that cannot be read is a warning. Existing lines and fields are unchanged.
- `init`: create a Coolify application for the current repository from its Git remote, then link it exactly as `link` would. The remote and branch come from Git, the build pack from the application root (`Dockerfile` or Nixpacks; Docker Compose is refused), and the plan — repository, source, branch, build pack, port, project, environment, server, name — is confirmed before anything is created, or requires `--yes`. `--project` with `--create-project` creates a missing project; `--server` names the server, otherwise the only usable one is used; `--deploy` submits and observes the first deployment. A directory that is already linked is refused.
- `init` for private repositories. `--source` (default `auto`) probes the remote with an anonymous `git ls-remote`, run in an empty temporary home with credential helpers, the user's Git configuration, and Git's own environment variables disabled so that no stored login (a credential helper, `~/.netrc`, an `http.extraHeader`) can answer for it, and given up on after 45 s even when the host never answers: a readable repository is created as public, an unreadable one through one of Coolify's private sources, asked for interactively or named with `--source github-app` or `--source deploy-key`. A GitHub App is picked with `--github-app NAME` or from the list, and its access to the repository is checked through Coolify before anything is created, as is the branch when the app's listing of them is complete (Coolify relays GitHub's first page of 30; a branch beyond it is left for the first deployment). `--source public` skips the probe. A deploy key is picked with `--deploy-key NAME` or from the list, never implicitly, and the repository is stored in its SSH form for it. `--create-deploy-key NAME` generates an Ed25519 key, registers it in Coolify, prints the public half once so it can be added to the repository, and stops; the private half is never printed or written. The plan and the result name the source; the JSON of a public plan is unchanged.
- A documentation site under `docs/`, built with Fumadocs (Next.js, MDX) and served as a Node.js server from a Docker image for deploying on Coolify at coolship.itrocas.com: an overview, a get-started path, concepts, a reference page per command derived from `--help` (including `init`, `domain`, `login`, and `logout`), guides for CI, monorepos, and environment variables, and the platform limits. Every page is also served as Markdown (`/docs/<page>.md`, or `Accept: text/markdown`) with `llms.txt` and `llms-full.txt` for assistants, and pages carry copy-as-Markdown and open-in-Claude/ChatGPT actions. The image is built by vinext (`docs/Dockerfile`), with a `next build` image (`docs/Dockerfile.next`) as the fallback; the `docs-site` workflow builds both images on pushes to `main` and pull requests that touch `docs/` and smoke-tests their routes.
- Refusals of creation and other mutations now carry the server's explanation (its `message` and field errors) in the error, so a repository Coolify cannot reach or a rejected value is reported as such rather than as a bare status code. Reads and server faults still report the status alone.
- Shell completion: `coolship completion bash|zsh|fish|powershell` prints a script for the shell, and the README says where each shell loads it from. An unknown shell name or an extra argument is invalid input (exit 2), like every other command's.
- `preview` takes the target positionally, like `deploy`: `coolship preview api --pr 42`.
- `login`: the global `--context` names the instance being saved when `--name` is absent, and a line before the token prompt says where to create the token (Keys & Tokens; read, write, deploy; sensitive read for build logs and secret values).
- `help` reaches nested commands: `coolship help env pull`, `coolship help domain set`.

### Changed

- `env diff --exit-code` now exits 1 with no further message when there are differences, like `git diff --exit-code`; the diff itself is still printed. It used to add an `Error: variables differ` line.
- `link` and `init` use the instance marked default in the Coolify CLI configuration without asking, exactly as every later command does; `--context` overrides it, and only a file with no default prompts for a choice (or, noninteractively, requires `--context`).
- A rejected token (HTTP 401), a token missing an ability (403), and an `http://` URL the server redirects (3xx) are explained on every command, not only by `doctor` and `login`: the status is kept and a hint follows it once — run `coolship login` or check `COOLSHIP_TOKEN`; the token needs read, write, and deploy; use the https URL.
- Transport failures name their cause in fixed words — the host name could not be resolved, the connection was refused, the TLS certificate could not be verified, the server did not answer with TLS or refused the handshake, the request timed out — instead of `request failed`, and never repeat the transport's own text.
- Missing credentials are reported as `No Coolify credentials at PATH; run coolship login, or set COOLSHIP_URL and COOLSHIP_TOKEN` by every command, `doctor` included (it used to say `run coolify login`); a file with no default instance says to pass `--context NAME` or run `coolship login --default`.
- `logs --lines` is checked against the server's range (1 to 10000) before any request, and the help says preview containers cannot be tailed.
- A deployment that outlives `--timeout` says so by name (`--timeout 10m0s elapsed before the deployment finished; it continues on the server`) instead of `context deadline exceeded`; a single request that times out while the deployment is observed still says `request timed out`, since the flag has not elapsed. A 429 from `POST /deploy` is reported as `server deployment queue is full`; a receipt saying a deployment of this commit is already queued is treated as a refusal even when it carries a UUID (Coolify never queues that one) and suggests `--force`, for `preview` too, since Coolify applies that check per pull request; and a deployment the server no longer holds while it is being observed (HTTP 404) is reported as dropped, with the same suggestion, instead of as `may still be running`.
- `unlink` in a monorepo lists every target the file holds in its confirmation, instead of an empty binding; the README says unlink removes every target.
- `env push` shows the withheld keys it skips and the remote-only keys it keeps in the confirmation, before the push rather than only after it. The order of the listed changes is fixed (create, update, delete), and `config` lists overrides in a fixed order.
- `dev` with more than one argument and no `--` says to put the command after `--`, with the corrected invocation.
- `link`'s own selection failures name the flags to use (`link: no project named "x" in …; pass an exact name with --project, or a UUID with --project-uuid`) instead of advising to run `coolship link` or to link a UUID, which only applies to later commands.
- `login` validates the URL before asking for the token: a bare host is asked again interactively, and noninteractively the error names `--url` or `--name`.

### Fixed

- `env pull` appended the same "withheld by Coolify" comment on every run; the note is now written once per withheld key, and a key already present locally still gets none.
- `logs` on an application with no running container reported a bare `HTTP 400 Bad Request`; it now says `application is not running (status exited:unhealthy)`, and `--follow` stops with the same message, naming the status read at that moment, if the container goes away.
- `deploy` and `preview` with `--format json` printed nothing when the deployment failed or timed out; the result, with the deployment UUID and its last observed status, is now printed before the failing exit status. Human output is unchanged.
- `/dev/null` counted as a terminal, so `coolship open </dev/null` tried to launch a browser and `unlink </dev/null` reported a cancelled prompt (exit 130); both now treat that input as noninteractive (`open` prints the URL only; `unlink` requires `--yes`, exit 2).
- A binary installed with `go install …@vX.Y.Z` reported `coolship dev`; it now reports the module version.
- `doctor`'s server check and `login`'s failure keep the `HTTP 401`/`403` status text alongside the explanation.

### Changed

- The one-page site under `site/` and its GitHub Pages workflow are replaced by the documentation site under `docs/`. GitHub Pages is no longer updated; the page it last built stays up at joaomnuno.github.io/coolship until Pages is disabled in the repository settings.

## [0.2.0] - 2026-09-10

Every command is now installable from a release, and the tool logs you in itself.

### Added

- `login` and `logout`: save a Coolify instance and API token after verifying them against the server, in the same file coolify-cli uses, so both tools share one login. The token is read without echo, or from stdin with `--token-stdin`; it is never a flag.
- A `LICENSE` file (MIT).
- Tagged releases: pushing a `vX.Y.Z` tag publishes a GitHub Release with archives for Linux, macOS, and Windows on amd64 and arm64, a `checksums.txt`, and notes taken from this changelog. Tags with a pre-release suffix such as `v0.2.0-rc.1` are published as pre-releases, and `latest` always points at the newest full release.
- A rolling `nightly` pre-release built from `main` on demand, replaced on every run and never marked `latest`.
- Continuous integration on every push and pull request: `gofmt`, `go vet`, tests with and without the race detector, builds, `shellcheck` on the scripts, and a check that pull requests changing Go code also update this changelog unless labeled `skip-changelog`.
- `scripts/install.sh`: a curl-able installer for Linux and macOS that downloads a release archive from GitHub Releases, verifies its SHA-256 against `checksums.txt`, installs into `$HOME/.local/bin` (or `COOLSHIP_INSTALL_DIR` / `--dir`), and prints a PATH hint for bash, zsh, or fish. `COOLSHIP_VERSION` / `--version` selects a release, including pre-releases; `--dry-run` shows what would happen. It never runs `sudo`.
- `scripts/e2e` and the `e2e` GitHub Actions workflow: every command is exercised against a live Coolify instance, confined to one example application, weekly and on each published release. The workflow is skipped when the `COOLSHIP_URL` and `COOLSHIP_TOKEN` secrets are absent.
- Styled human output on terminals: `doctor` markers, deployment statuses, `Warning:` and `Error:` prefixes, prompt questions, `env diff` markers, and key/value labels are colored with plain ANSI sequences. Styling is decided per stream, so a piped stdout stays plain while prompts on a terminal stderr are styled; `NO_COLOR`, `TERM=dumb`, `CI`, and the new `--no-color` flag turn it off. `--format json` and streamed logs are never styled.
- A one-page site in `site/`, published to GitHub Pages from `main` and buildable as an nginx image for deploying on Coolify.

## [0.1.0] - 2026-09-09

First release. Every command from the original brief is implemented and was
verified end to end against a live Coolify **4.3.18** instance, using a public
example repository and an isolated project on that instance.

### Added

- `link`: bind a repository to an existing Coolify application, interactively or with explicit selectors, writing a credential-free `coolship.toml`. Names are matched exactly within their parent; UUID pins are written when a name is ambiguous.
- `status`, `deploy`, `logs`: inspect, deploy, and read runtime logs of the linked application. `deploy` observes exactly the deployment it submitted and streams the server's build log; `logs --follow` polls snapshots and reports gaps rather than hiding them.
- `open`: print or open the application's URL, or its Coolify page with `--dashboard`.
- `unlink`, `config`, `doctor`: remove the binding, show the effective configuration, and check every preparation step with a failing exit status.
- `env pull`, `env diff`, `env push`: synchronize a local dotenv file with one scope of the application's variables. Preview scope is explicit, withheld values are never invented, shared references are never flattened, and pushed updates preserve flags the server would otherwise reset.
- `preview`: deploy the preview Coolify holds for a pull request, from `--pr` or `GITHUB_REF`.
- `dev`: run a local command with the application's runtime variables injected, exit status and signals propagated.
- `domain`, `domain set`: show and replace the application's domains, reading the application back to confirm.
- Named monorepo targets: `[apps.<name>]` tables selected by directory or by name, with add-without-review and change-with-review migration rules.
- Credentials from the Coolify CLI configuration, or from `COOLSHIP_URL` and `COOLSHIP_TOKEN` for CI. A `User-Agent` identifying Coolship.
- `--format json` on every command, with values masked by default where they may be secrets.

### Compatibility

- Verified against Coolify 4.3.18; the 4.3.19 source has no changes to any endpoint used. Other versions are untested.
- Requires Go 1.26 to build, matching `coolify-cli`.

[Unreleased]: https://github.com/joaomnuno/coolship/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/joaomnuno/coolship/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/joaomnuno/coolship/releases/tag/v0.1.0
