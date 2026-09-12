# Coolship

> A project-local developer CLI for Coolify.

Coolship aims to bring a **Wrangler-like developer experience** to [Coolify](https://coolify.io/), focused on the workflow between your local project and its deployed application.

Documentation: [coolship.itrocas.com](https://coolship.itrocas.com) — get started, concepts, a reference page per command, guides, and limits, also published as Markdown and `llms.txt` for assistants. The site lives in [`docs/`](docs/README.md).

Instead of repeatedly dealing with application UUIDs, projects, environments, and dashboard navigation, Coolship links a local repository to a Coolify application and lets you work with it directly from the terminal.

```bash
coolship link
coolship status
coolship deploy
coolship logs
coolship open
```

## Why?

Coolify already has [`coolify-cli`](https://github.com/coollabsio/coolify-cli), which provides command-line access to Coolify and its resources.

Coolship is **not intended to replace it** or become another general-purpose Coolify administration CLI.

The distinction is:

* **`coolify-cli`** manages Coolify resources.
* **Coolship** manages the developer workflow around the project you're currently working on.

The goal is for commands to understand the current repository automatically.

Instead of:

```bash
coolify deploy uuid <application-uuid>
```

the workflow could simply be:

```bash
cd my-project
coolship deploy
```

because the repository is already linked to the correct Coolify project, environment, and application.

## Status

🚧 **Early development.** `init`, `link`, `status`, `deploy`, `deployments`, `cancel`, `stop`, `start`, `restart`, `logs`, `open`, `unlink`, `config`, `doctor`, `env pull|diff|push`, `preview`, `dev`, and `login` are implemented, tested, and verified end to end against a live Coolify 4.3.18 instance — see [Server compatibility](#server-compatibility) for what that does and does not cover. `domain` is implemented and tested too; `scripts/e2e` reads it and exercises a no-op `domain set`, but no run against a live instance has confirmed either yet.

Ideas, feedback, and contributions are welcome.

## Install

On Linux (glibc or musl) and macOS, amd64 or arm64:

```bash
curl -fsSL https://raw.githubusercontent.com/joaomnuno/coolship/main/scripts/install.sh | sh
```

The script downloads the release archive from GitHub Releases, checks its SHA-256 against the release's `checksums.txt`, installs `coolship` into `$HOME/.local/bin`, and prints the installed version plus a one-liner for your shell if that directory is not on `PATH`. It needs `curl` or `wget`, `tar`, and `sha256sum` or `shasum`; it never runs `sudo` (if you point it at a system directory it prints the command to run instead). Windows is not supported by the script; build from source or use WSL.

Overrides, as environment variables or flags (`sh -s -- --version 0.3.0 --dir /opt/bin`):

| Variable               | Flag        | Default           | Meaning                                                              |
| ---------------------- | ----------- | ----------------- | -------------------------------------------------------------------- |
| `COOLSHIP_VERSION`     | `--version` | latest            | Release to install; a pre-release such as `0.3.0-rc.1` must be named |
| `COOLSHIP_INSTALL_DIR` | `--dir`     | `$HOME/.local/bin` | Directory to install into, created if missing                        |
|                        | `--dry-run` |                   | Resolve the version and print what would happen                      |

Binaries are published by the release workflow; until that has run for a tag (v0.1.0 was tagged before it existed), the script reports the archive as unpublished and building from source is the way to get that version.

### Build from source

Coolship needs Go 1.26 or newer, matching `coolify-cli` so code can move upstream. Newer toolchains build it unchanged.

```bash
git clone https://github.com/joaomnuno/coolship.git
cd coolship
scripts/build            # bin/coolship, version stamped from the nearest tag
bin/coolship --version
```

A plain `go build -o coolship .` also works and reports the Git revision it was built from, and `go install github.com/joaomnuno/coolship@latest` reports the module version it installed. Inside this repository, use `./scripts/go` instead of `go` so build and test caches stay in `.cache/` rather than your home directory. Releases are tagged `vX.Y.Z`; see [CHANGELOG.md](CHANGELOG.md).

### Shell completion

`coolship completion bash|zsh|fish|powershell` prints a completion script for your shell; `coolship completion zsh --help` shows where each shell loads it from. For example:

```bash
source <(coolship completion bash)                       # bash, in ~/.bashrc
coolship completion zsh > "${fpath[1]}/_coolship"        # zsh, then start a new shell
coolship completion fish > ~/.config/fish/completions/coolship.fish
```

### Releases

Every `vX.Y.Z` tag publishes a [GitHub Release](https://github.com/joaomnuno/coolship/releases) with `coolship_<version>_<os>_<arch>.tar.gz` archives (`.zip` on Windows) for Linux, macOS, and Windows on amd64 and arm64, plus a `checksums.txt` of SHA-256 sums. Tags with a suffix such as `v0.3.0-rc.1` and the rolling `nightly` build from `main` are marked as pre-releases; `latest` always points at the newest full release.

## Getting started

Log in once. Coolship verifies the URL and token against the server, then stores them in the same file [`coolify-cli`](https://github.com/coollabsio/coolify-cli) uses — so if you have already authenticated there, skip this step, and if you log in here, coolify-cli is logged in too.

```text
$ coolship login
Coolify URL: https://coolify.example.com
Context name [coolify]: home
Create a token in Coolify under Keys & Tokens with read, write, and deploy; add sensitive read to see build logs and secret values.
API token:
Logged in to home (https://coolify.example.com) as team Personal on Coolify 4.3.18, now the default
Saved to /home/you/.config/coolify/config.json
```

Create the token in Coolify under your profile's **Keys & Tokens** page with the *read*, *write*, and *deploy* abilities; build logs and secret values are also withheld unless the token has *sensitive read*. The URL must be the full `https://…` address — a bare host is asked again — and the token is never echoed and never accepted as a flag. For CI, either set `COOLSHIP_URL` and `COOLSHIP_TOKEN` (no login needed) or pipe the token: `echo "$TOKEN" | coolship login --url … --name ci --token-stdin` (`--context ci` names it too, and without a terminal a missing `--url` or `--name` is an error rather than a prompt). `coolship logout NAME` removes a context.

Then link a repository. If it is already an application on Coolify, `link` binds it; if it is not on Coolify yet, `init` creates the application from the repository's remote — public, or private through a GitHub App or a deploy key — and binds it in one step:

```bash
cd my-app
coolship link          # bind to an existing application
coolship init          # or: create the application first, then bind
```

`link` walks the hierarchy, asking only when a choice is genuinely ambiguous:

```text
Select project:
  1. Personal (a1b2c3d4)
  2. Work (e5f6a7b8)
Choice [1-2, q to cancel]: 1

Linked project in /home/you/my-app/coolship.toml
Application: fenix-bot (9f8e7d6c)
Environment: production
Project: Personal
Context: home
```

Every later command reads that binding, so none of them need resource identifiers:

```bash
coolship status
coolship deploy
coolship logs --follow
```

## Commands

### `coolship init`

Create a Coolify application for the current repository, then link it — the first step for a repository that is not on Coolify yet. `init` reads the `origin` remote and the checked-out branch, checks whether the repository can be read without credentials, detects the build pack from the application root, shows the plan, and creates the application only after you confirm:

```text
$ coolship init
Create application my-app on home?
  Repository:  https://github.com/you/my-app (branch main)
  Source:      public (cloned without credentials)
  Build pack:  dockerfile, port 80
    Dockerfile: /Dockerfile
  Project:     Personal
  Environment: production
  Server:      Master Ubuntu
  Binding:     /home/you/my-app/coolship.toml
Confirm [y/N]: y
Created application my-app (9f8e7d6c) from https://github.com/you/my-app at main
Build pack: dockerfile, port 80
Dockerfile: /Dockerfile
URL: https://9f8e7d6c.coolify.example.com
Linked project in /home/you/my-app/coolship.toml
```

The project and server are asked for only when there is a choice; `--project NAME` selects one, `--create-project` creates it when missing, `--server NAME` picks the server, and `--environment` defaults to `production`. `--repo`, `--branch`, and `--name` override what Git said. Noninteractive use needs `--yes`:

```bash
coolship init --project Personal --server "Master Ubuntu" --port 8080 --yes
```

**Build packs.** The application root decides, in the order Coolify's own form checks: a compose file (`docker-compose.yaml`, `docker-compose.yml`, `compose.yaml`, or `compose.yml`) makes a Compose application, a `Dockerfile` builds itself, an `index.html` with no `package.json` is served as it is, and anything else goes to Railpack, Coolify's default. `--build-pack` names one of `railpack`, `nixpacks`, `static`, `dockerfile`, or `dockercompose` instead. Each pack has its own flags, and one that does not belong to the pack is refused before any request:

* `railpack` and `nixpacks` build an image and run it on `--port` (default 3000). `--install-command`, `--build-command`, and `--start-command` override what they detect. `--static` serves the build output with nginx on port 80 from `--publish-dir` (default `/dist`).
* `static` serves the files as they are, with no build, on `--port` (default 80); `--publish-dir` serves a subdirectory.
* `dockerfile` builds the `Dockerfile` in the root, or the one `--dockerfile PATH` names, and runs it on `--port` (default 80). Coolify's own health check is switched off at creation, as its form does, because the check it would generate needs `curl` or `wget` in the image; a `HEALTHCHECK` in the Dockerfile is still used.
* `dockercompose` runs the compose file found, or the one `--compose-file PATH` names. Each service publishes the ports the file gives it, so `--port` is refused. `--compose-domain SERVICE=URL`, repeated per service, gives the services their domains; without one the plan warns, and the domains are set in Coolify afterwards, since `domain set` does not apply to Compose applications.

**Check the port**: Coolify routes traffic to the one in the plan.

```bash
coolship init --static --publish-dir build --yes                  # railpack builds, nginx serves build/
coolship init --dockerfile deploy/Dockerfile --port 8080 --yes
coolship init --compose-domain web=https://app.example.com --compose-domain api=https://api.example.com --yes
```

Nothing is deployed unless you pass `--deploy`, which then submits and observes the first deployment exactly like `deploy`. A directory that is already linked is refused rather than re-pointed; use `link` to change a binding.

**Private repositories.** How Coolify clones is decided by `--source`, whose default `auto` runs an anonymous `git ls-remote` against the remote (run in an empty temporary home with credential helpers, the user's Git configuration, and Git's own environment variables disabled, so no stored login — a credential helper, `~/.netrc`, an `http.extraHeader` — can make a private repository look public; a host that never answers is given up on after 45 s). A repository that answers is public and is cloned without credentials (`POST /applications/public`). One that does not is private, and `init` asks which of Coolify's two private sources to use, or needs `--source github-app` or `--source deploy-key` when input is noninteractive. Both are chosen by name from what Coolify already holds, the choice is shown in the plan, and neither is ever taken implicitly:

* **GitHub App** (`--github-app NAME`, or pick from the list): a GitHub App installed on the repository and registered in Coolify under *Sources*. Before anything is created, `init` lists the repository's branches through Coolify as the app sees them, so a missing installation fails with a clear message rather than a half-configured application, and so does a branch that a complete listing lacks. GitHub answers 30 branches at a time and Coolify relays the first page only, so a branch beyond it cannot be checked and is left for the first deployment to find; `init` says so. Created with `POST /applications/private-github-app`.
* **Deploy key** (`--deploy-key NAME`, or pick from the list): an SSH key Coolify holds, registered on the repository as a read-only deploy key. A deploy key clones over SSH, so the repository is stored in its SSH form — `git@github.com:owner/repo.git`, or `git@host:2222/owner/repo.git` with a port; an `origin` that already is an SSH remote keeps its user and port. Created with `POST /applications/private-deploy-key`. The list also holds keys Coolify made for its servers, which is why a key is asked for even when there is only one.

  When no suitable key exists, `--create-deploy-key NAME` generates an Ed25519 pair, registers the private half in Coolify (`POST /security/keys`), prints the public half once on stdout, and stops without creating the application:

  ```text
  $ coolship init --create-deploy-key my-app-deploy
  Create deploy key my-app-deploy on home for git@github.com:you/my-app.git?
    Repository:  git@github.com:you/my-app.git (branch main)
    Source:      deploy key my-app-deploy (new; the application is created once the key is registered on the repository)
  Confirm [y/N]: y
  Created deploy key my-app-deploy (k1l2m3n4) on home
  Public key:
  ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI… my-app-deploy

  Add it to git@github.com:you/my-app.git as a read-only deploy key, then create the application with:
    coolship init --source deploy-key --deploy-key my-app-deploy
  ```

  Add that line under the repository's *Settings → Deploy keys* (or `gh repo deploy-key add key.pub`), then run the printed command. The private half never leaves Coolify: it is not written to disk, not printed, and not part of `--format json`.

`--github-app`, `--deploy-key`, and `--create-deploy-key` each imply their source; `--source public` skips the probe and lets Coolify clone anonymously, which fails at deployment if the repository is private.

### `coolship link`

Bind the repository to an **existing** Coolify application and write `coolship.toml`. Linking only changes local configuration; it never creates remote resources.

Supply selectors to run without prompts, which is what CI needs:

```bash
coolship link --project Personal --environment production --application fenix-bot
```

Names are matched exactly within their selected parent. Use `--project-uuid`, `--environment-uuid`, or `--application-uuid` to pin identity instead of a name; `link` writes a pin automatically when a name cannot describe the selection uniquely.

Replacing a different existing binding requires confirmation, or `--replace` when noninteractive. Re-running `link` with the same result leaves the file, including its comments, untouched.

### `coolship status`

Report the linked application's current status and URL, and its last deployment when the history can be read.

```text
$ coolship status
Application: coolship-example (mm4c0zpbrzx8z96t0qiw3tff)
Environment: production
Project: coolship-example
Context: home
Status: running:healthy
URL: https://coolship.example.com
Last deployment: nmfvbbn3 finished (0cd7c4a) 2026-09-10 11:37:12
```

### `coolship deploy`

Deploy the linked application using the source and branch **already configured in Coolify**. This does not upload your worktree or push local commits.

```bash
coolship deploy                  # wait for the deployment to finish
coolship deploy --no-wait        # return the queued deployment UUID
coolship deploy --force          # rebuild without cache
coolship deploy --timeout 20m
coolship deploy --logs           # stream the build log above the checklist
```

Coolship submits one deployment and then observes exactly the UUID that submission returned, never "the latest deployment", which could belong to someone else. In a terminal the deployment is a checklist: the target, then the deployment and its stages — build, rolling update, container, cleanup — ticking as Coolify's build log markers arrive, with a spinner and elapsed time on whatever is open. The build log stays collapsed and is printed in full if the deployment fails; `--logs` streams it live, `--no-logs` keeps it collapsed, and without either the `build_logs` preference decides. Interrupting the command stops local waiting only; the remote deployment continues, and the UUID is reported so you can pick it back up.

```text
$ coolship deploy
→ coolship-example
→ production

✓ Deployed                      0:52
  ✓ build                       0:41
  ✓ rolling update              0:08
  ✓ container                   0:06
  ✓ cleanup                     0:00
Deployment: 03dusayin5rleswixblvdqba
Application: coolship-example (mm4c0zpbrzx8z96t0qiw3tff)
Status: finished
https://coolship.example.com
```

Piped, in CI, or with `--format json`, the status lines, the build log — clone, build, rolling update, health checks — and one line per stage transition (`Stage build: started`) stream to stderr as they arrive, when your token may read the log; otherwise progress continues without it.

The last line is where to look: the application's URL when the deployment finished and the application has a domain, otherwise the deployment's page in Coolify — with `--no-wait`, when `--timeout` elapses, and for a preview, whose application URL is the production one. A failed deployment prints the build log it gathered, then names its page on stderr, where the retry lives.

When `--timeout` elapses the error names the flag (`--timeout 10m0s elapsed before the deployment finished; it continues on the server`), and when the deployment fails or times out, `--format json` still prints the result with the deployment UUID and its last observed status before exiting 1, so a script can pick the deployment up. If Coolify already holds a queued or running deployment for the same commit it declines a new one with `Deployment already queued for this commit.`, which Coolship reports with the suggestion to pass `--force`; a second submission within a couple of seconds can instead be accepted and then dropped by the server, which observation reports as `the server no longer holds this deployment (HTTP 404)`. When the server's deployment queue is full it answers 429, reported as `server deployment queue is full`.
### `coolship deployments`

List the linked application's most recent deployments, newest first — the same history Coolify shows on the application's Deployments page.

```text
$ coolship deployments -n 3
Deployments of coolship-example (3 of 17)
UUID      STATUS             COMMIT   TYPE     CREATED              DURATION
jky1r9cr  finished           0cd7c4a  restart  2026-09-10 11:56:59  23s
zvvmfq5q  cancelled-by-user  HEAD     deploy   2026-09-10 11:56:21  2s
ivgyhyfx  finished           0cd7c4a  deploy   2026-09-10 11:55:31  23s
```

`TYPE` is `deploy`, `restart`, `rollback`, or `preview #N` for a pull request preview. A commit of `HEAD` is a deployment Coolify cancelled or is still starting, before it resolved the sha; a cancelled deployment that never reached the deployment job has no duration, because Coolify records the end time only there. `--format json` carries the full UUIDs, the source (`api`, `webhook`, or `manual`), and the server's timestamps. Build logs are never included; `deploy` streams them while a deployment runs.

### `coolship cancel`

Cancel a queued or running deployment.

```bash
coolship cancel                               # the one in progress; asks first
coolship cancel zvvmfq5q7kdzaswmsf7yrugt --yes  # a specific deployment
coolship cancel api zvvmfq5q7kdzaswmsf7yrugt   # a monorepo target, then the UUID
```

Without a UUID, exactly one deployment must be queued or in progress; none, or more than one, is reported and nothing is cancelled. A named deployment must belong to the linked application. Only queued and in-progress deployments can be cancelled — one that already finished, failed, or was cancelled is refused before any request. Cancelling leaves the running containers as they are: the application keeps serving the previous deployment.

### `coolship stop`, `coolship start`, `coolship restart`

Manage the lifecycle of the linked application's containers.

```bash
coolship stop                 # asks; --yes to skip
coolship stop --timeout 30s   # wait this long for the status to leave running
coolship start                # deploy again, observed like deploy
coolship restart --yes        # queue a restart, observed like deploy
```

`stop` shows the application and its environment and asks before stopping — production deserves a clear question — then waits until the status reports `exited` (2 minutes by default) and reports the last status it saw. Coolify stops and removes the containers; the application, its configuration, and its history stay, and `deploy` or `start` brings it back. Only an application that already reports `exited` is left alone, with a warning; every other status is stopped, as Coolify's own Stop button does — a crash-looping application reports `restarting` or `degraded`, never `running`, and is the usual reason to reach for the command.

```text
$ coolship stop --yes
Application stopping request queued.
Application status: exited:unhealthy
Application: coolship-example (mm4c0zpbrzx8z96t0qiw3tff)
Status: exited:unhealthy
```

`start` and `restart` are Coolify's own start and restart actions, and both queue a deployment: Coolify has no container start, so `start` deploys the configured source and branch again, and `restart` queues a restart-only deployment that reuses the image already built for the commit — except for Dockerfile and Docker image applications, which Coolify deploys in full. Both are observed exactly like `deploy` — the stage checklist in a terminal, the status lines and build log on stderr otherwise — with `--no-wait`, `--timeout`, `--logs`, and `--no-logs`; `restart` asks first, or takes `--yes`.

### `coolship logs`

Read runtime logs from the linked application.

```bash
coolship logs
coolship logs --lines 500
coolship logs --follow
```

The server reads the application's first container and returns at most 10000 lines per snapshot; `--lines` is checked against that range before any request is made. Pull request preview containers cannot be tailed. An application with no running container has no logs, so instead of the server's bare HTTP 400 Coolship reports `application is not running (status exited:unhealthy)`, and `--follow` stops with the same message if the container goes away.

### `coolship open`

Open the application's public URL — or, with `--dashboard`, its page in Coolify — in your default browser. The URL is always printed on stdout, and nothing is launched when stdin is not a terminal (a redirect from `/dev/null` included) or with `--print`, so it composes with other tools.

```bash
coolship open
coolship open --dashboard
coolship open --print | pbcopy
```

### `coolship doctor`

Run every step a command performs and report each one: configuration, Git boundary, binding, credentials, context, server reachability and version, and whether the binding resolves to a running application. Exit status is 1 when any check fails; warnings do not.

```text
$ coolship doctor
[ok]   Project configuration: /home/you/my-app/coolship.toml
[ok]   Git repository: /home/you/my-app
[ok]   Binding: Personal / production / fenix-bot in /home/you/my-app
[ok]   Credentials: /home/you/.config/coolify/config.json (1 instance, default home)
[ok]   Context: home at https://coolify.example.com
[ok]   Server: Coolify 4.3.18
[ok]   Application: fenix-bot (9f8e7d6c) is running:healthy
```

### `coolship config`

Show the effective configuration for this directory after overrides: the discovered file, target, binding, and which credentials would be used. It reads files only — no request is made and no token is shown.

### `coolship unlink`

Delete `coolship.toml` — in a monorepo, that removes every `[apps.<name>]` target in it, and the confirmation lists them. Nothing on the server changes. Deletion asks for confirmation, or requires `--yes` when noninteractive, and refuses if the file changed since it was read.

### `coolship preview`

Deploy the preview Coolify holds for a pull request, and observe it exactly like `deploy`.

```bash
coolship preview --pr 42
coolship preview api --pr 42   # a named monorepo target, like deploy
coolship preview               # in a GitHub Actions pull_request job, reads GITHUB_REF
```

Coolify must already know the pull request: enable *Preview Deployments* on the application and let Coolify's GitHub webhook (or its UI) register the PR. The API offers no way to create a preview, so this command cannot either — when the server does not know the PR it says so, and Coolship repeats that answer with what to do about it. Verified live against a public repository through the webhook path.

### `coolship domain`

Show the linked application's domains — Coolify generates one from the application UUID until you set your own — and replace them.

```bash
coolship domain
coolship domain set app.example.com                       # bare host means https://
coolship domain set https://app.example.com https://www.example.com --redirect non-www
```

`set` shows the change and asks first (`--yes` when noninteractive), refuses a domain Coolify sees in use elsewhere unless `--force`, reads the application back to confirm what the server kept, and reminds you that the proxy learns the new domain on the next deployment. Docker Compose applications take per-service domains, which this command does not set.

### `coolship dev`

Run a local command in the application root with the application's **runtime** variables injected over your environment, so a process sees what it would see on Coolify — without pulling a `.env` file first.

```bash
coolship dev -- npm run dev
coolship dev api -- go run .
coolship dev                       # runs the binding's dev setting
```

```toml
[project]
dev = "npm run dev"
```

A command after `--` runs directly; the configured `dev` string runs through your shell. Before `--` only a target name is accepted, so `coolship dev npm run dev` is refused with the form to use. Unlike `env pull`, `dev` injects shared references as the values they resolve to. Withheld values are reported and left to your own environment. The command's exit status becomes Coolship's, and Ctrl-C is forwarded to it.

### `coolship env`

Synchronize a local dotenv file with the linked application's variables. The file defaults to `.env` in the application root and is created with private permissions.

```bash
coolship env pull                 # remote → .env, keeping local-only keys and comments
coolship env diff                 # what push would change, values masked
coolship env diff --show-values
coolship env diff --exit-code     # status 1 when push would change anything, like git diff
coolship env push                 # create and update; asks first
coolship env push --prune --yes   # also delete remote-only keys, without asking
```

Three rules keep this safe:

* **Two scopes.** Coolify keeps a separate copy of every variable for preview deployments. Commands act on the regular scope unless `--preview` is given, and never touch the other one — though Coolify itself creates a preview copy of any regular variable that `push` creates, so a later `--preview` diff will show them.
* **Withheld values are never invented.** A value Coolify hides (shown-once secrets) is noted in the file as a comment rather than written empty, is reported as `?` in a diff, and is overwritten by `push` only with `--force`.
* **References stay references.** A shared variable such as `{{team.API_KEY}}` is pulled and compared as that reference, never as the value it resolves to, so a push cannot replace the reference with the secret.

`push` preserves each variable's literal, multiline, and shown-once flags — the server resets them when an update omits them. Its confirmation lists what it creates, updates, and deletes, and also the withheld keys it skips (`--force` overwrites them) and the remote-only keys it keeps (`--prune` deletes them), so nothing is a surprise afterwards. Changes take effect on the next deployment.

`env diff --exit-code` exits with status 1 when there are added, changed, or removed keys — withheld keys do not count — and prints the diff with no further message, like `git diff --exit-code`, so CI can fail when `.env` drifts. A repeated `pull` leaves one comment per withheld key, not one per run.

### Shared options

| Flag | Purpose |
| --- | --- |
| `--cwd` | Act on this directory without changing the process directory. |
| `--config` | Use an explicit project configuration path. |
| `--context` | Use this Coolify CLI instance for this invocation; `login` saves under this name when `--name` is absent. |
| `--coolify-config` | Read credentials from an explicit Coolify CLI configuration file. |
| `-e`, `--environment` | Override the remote environment for this invocation. |
| `-t`, `--target` | Select a named target in a monorepo configuration. |
| `--format` | `human` (default) or `json`. |

Overrides apply to a single invocation and never rewrite `coolship.toml`.

## Project configuration

`link` writes a small, credential-free, versioned file at the repository root:

```toml
version = 1

[project]
context = "home"
project = "Personal"
environment = "production"
application = "fenix-bot"
root = "."
```

`context` names a locally configured Coolify instance; it is neither a URL nor a token. `root` is the application root relative to the configuration directory. Optional `project_uuid`, `environment_uuid`, and `application_uuid` fields pin identity when names are ambiguous or when you ask for a pin. A pinned UUID is authoritative: if it disappears, Coolship reports that rather than falling back to a same-named replacement.

Commit this file. **Tokens and secret values are never written to it.**

Configuration is discovered by searching upward from the working directory, stopping at the enclosing Git worktree root so a project never picks up a parent repository's binding.

### Monorepos

A repository with several applications uses named targets instead of `[project]`:

```toml
version = 1

[apps.web]
context = "home"
project = "Personal"
environment = "production"
application = "frontend"
root = "apps/web"

[apps.api]
context = "home"
project = "Personal"
environment = "production"
application = "backend"
root = "apps/api"
```

Link each target from its own directory, which becomes its root:

```bash
cd apps/web && coolship link --target web
cd ../api  && coolship link --target api
```

Commands then pick the target whose root most specifically contains the current directory, so `cd apps/api && coolship deploy` deploys the API. From anywhere, name it instead: `coolship deploy api`, `coolship logs web`, or `--target api` on any command. Two targets with the same root, or a directory outside every root, require an explicit name rather than a guess.

Rules that keep the file unambiguous: a file uses `[project]` **or** `[apps.<name>]`, never both; adding a target keeps the existing ones and needs no confirmation; changing a target, or converting between the two forms — which drops the other form's bindings — asks for confirmation or `--replace`.

## Credentials

Coolship never asks you to authenticate twice. `coolship login` writes, and every command reads, the Coolify CLI configuration:

* Unix and macOS: `~/.config/coolify/config.json`
* Windows: `%APPDATA%\coolify\config.json`

The instance is selected by `--context`, then the committed `project.context`, then the single default instance; `link` and `init`, which have no committed context yet, use the default the same way and ask which instance only when the file has no default. A missing or ambiguous choice is an error rather than a guess: with no credentials at all every command says `No Coolify credentials at ~/.config/coolify/config.json; run coolship login, or set COOLSHIP_URL and COOLSHIP_TOKEN`, and with several instances and no default it says to pass `--context NAME` or run `coolship login --default`.

Tokens are created in Coolify under **Keys & Tokens** with the *read*, *write*, and *deploy* abilities; build logs and secret values additionally need *sensitive read*, and without it they are reported as withheld rather than empty.

For CI, where Coolify CLI may not be installed, supply the pair:

```bash
export COOLSHIP_URL=https://coolify.example.com
export COOLSHIP_TOKEN=…
coolship deploy
```

Both are required together, and the pair cannot be combined with `--context` or `--coolify-config`. It overrides any committed context for that invocation, and Coolship says so in a warning.

## Output and exit codes

Results go to stdout; prompts, progress, and diagnostics go to stderr, so piping stdout stays useful. `--format json` prints one result object, and `logs --format json` prints newline-delimited events.

Human output is colored only when the stream it goes to is a terminal, and each stream is decided on its own, so `coolship status | cat` prints plain text while a prompt on the terminal is still styled. Set [`NO_COLOR`](https://no-color.org) or pass `--no-color` to turn styling off everywhere; `TERM=dumb` and `CI` do the same. JSON output and the server's logs are never styled.

A failure is one `Error:` line on stderr. Three server answers get the same explanation on every command, appended once: a rejected token (`HTTP 401 Unauthorized; the server rejected the token; run coolship login …`), a token missing an ability (`HTTP 403 Forbidden; the token lacks a required ability …`), and an `http://` URL the server redirects (`HTTP 301 Moved Permanently; use the https URL; redirects are not followed`). Transport failures are named in fixed words — the host name could not be resolved, the connection was refused, the TLS certificate could not be verified, the server did not answer with TLS, the request timed out — and never repeat the transport's own text, which can carry a URL.

Two commands answer with a status and no message, like `git diff --exit-code`: `env diff --exit-code` when there are differences (the diff is the message), and `dev`, which exits with its child's status after the child has printed what it had to say. `deploy` and `preview` with `--format json` print the result, with the deployment UUID and its last observed status, before exiting 1 when a deployment fails or `--timeout` elapses.

| Code | Meaning |
| --- | --- |
| `0` | Success. |
| `1` | The operation failed; also `env diff --exit-code` with differences. |
| `2` | Invalid input, configuration, or selection. |
| `130` | Interrupted. |

## Server compatibility

**Verified against Coolify 4.3.18.** Every command was run end to end against a live instance: creating Dockerfile applications with `init` from a public repository, through a GitHub App, and through a deploy key `init` generated (deployed once to prove the key clones) and deleting them again, linking, deploying, and following logs of a real Dockerfile application, syncing its variables in both scopes, deploying a webhook-created pull request preview, running a local process with its variables, and linking a two-target monorepo. The 4.3.19 source has no changes to any endpoint Coolship uses, so it is expected to behave identically; other versions are untested.

Limits worth knowing:

* Deployment states are interpreted as `queued`, `in_progress`, `finished`, `failed`, and `cancelled-by-user`. An unknown state is shown as-is and waits for the timeout instead of being guessed.
* `logs --follow` polls snapshots and compares overlapping lines. The endpoint has no cursor, so log rotation or a container restart can cause gaps or duplicates. Coolship reports the reset rather than pretending the stream is lossless.
* Selecting a container is not supported, because the server ignores the parameter the Coolify CLI sends for it.
* Build logs and secret values can be withheld by token ability and team role. Withheld data is reported as unavailable, never as empty data.
* An uncertain deployment submission is never retried automatically, since the API defines no idempotency key. Coolship reports what it knows so you can recover manually.

[ARCHITECTURE.md](ARCHITECTURE.md) records the verification and the server behaviors it uncovered.

## End-to-end tests

`scripts/e2e` runs most commands against a live Coolify instance and prints a PASS/FAIL line per step; it does not run `login`, `logout`, or `init`, and it exercises `preview` only through its refusal path, not a real deploy. It touches exactly one application — project `coolship-example`, environment `production`, application `coolship-example`, a Dockerfile app built from [joaomnuno/example-coolify-project](https://github.com/joaomnuno/example-coolify-project) — and nothing else on the instance is written to. In order it links from a fresh temporary directory, runs `doctor`, `status`, `config`, `open --print`, `logs` and `logs --follow`, pulls and diffs the variables, creates two `E2E_`-prefixed variables and verifies `dev` injects them, deletes them again with `env push --prune` (in both scopes; a trap does the same if the run dies mid-way), checks that `preview --pr 999999` is refused (it does not deploy a real preview), reads the application's domains and runs `domain set` with that same list, a no-op the server accepts without changing anything (a trap restores it the same way if the run dies mid-way), deploys once (about 30 s), lists the deployments, checks that `cancel` refuses when nothing is running, stops and starts the application, restarts it, and unlinks. The lifecycle steps leave the application running when they pass; if `start` fails after `stop`, the application stays stopped until the next deployment. Pulled variables only ever land in the temporary directory, which is removed on exit, and the token is never printed.

`.github/workflows/e2e.yml` runs it on `workflow_dispatch`, weekly, and on every published release. Add two repository secrets under *Settings → Secrets and variables → Actions*: `COOLSHIP_URL` and `COOLSHIP_TOKEN`, where the token needs read, write, and deploy abilities. Without them the job is skipped, so forks and pull requests never fail on it. Runs are serialized so two never overlap on the shared application.

Locally, export the same pair and run the script; it builds the binary itself unless `COOLSHIP_BIN` points at one. GNU `timeout` and `python3` are required.

```bash
export COOLSHIP_URL=https://coolify.example.com
export COOLSHIP_TOKEN=…
scripts/e2e
```

## Planned

Every command from the original brief is implemented, and `init` creates applications from public and private repositories. Remaining directions are tracked in [ROADMAP.md](ROADMAP.md): sharing packages with `coolify-cli` once their interfaces settle.

## What Coolship is not

Coolship is not intended to become a replacement interface for every Coolify API resource.

Commands such as these are deliberately outside the main scope:

```text
server create
server delete
private-key list
team members
```

Those operations belong in a general-purpose administration tool such as `coolify-cli`.

Coolship should remain focused on questions like:

> "I'm inside this project. How do I deploy it?"

rather than:

> "How do I administer my Coolify instance?"

## Built with Go

Coolship is being developed in **Go**, with compatibility with the existing Coolify ecosystem in mind.

Using Go also leaves open the possibility of contributing parts of the project upstream to `coolify-cli` in the future if the workflows prove useful to the wider Coolify community.

## Goals

* Make deploying from a local repository fast and obvious.
* Avoid repeatedly copying Coolify resource UUIDs.
* Reuse existing Coolify authentication/context where possible.
* Provide good terminal UX for deployments and logs.
* Make environment-variable workflows safer and easier.
* Support monorepos and multiple applications over time.
* Stay complementary to `coolify-cli` rather than duplicating it.
* Keep the architecture suitable for possible upstream integration later.

## Inspiration

Coolship is heavily inspired by developer-focused CLIs such as Cloudflare's **Wrangler**, where the CLI understands the project you're currently working on and provides a smooth path from local development to deployment.

The goal is to bring that style of workflow to Coolify.

---

**Coolship — link once, then ship.**

## License

[MIT](LICENSE).
