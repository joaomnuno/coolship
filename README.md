# Coolship

> A project-local developer CLI for Coolify.

Coolship aims to bring a **Wrangler-like developer experience** to [Coolify](https://coolify.io/), focused on the workflow between your local project and its deployed application.

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

🚧 **Early development.** `link`, `status`, `deploy`, `logs`, `open`, `unlink`, `config`, `doctor`, `env pull|diff|push`, `preview`, `dev`, and `domain` are implemented, tested, and verified end to end against a live Coolify 4.3.18 instance — see [Server compatibility](#server-compatibility) for what that does and does not cover.

Ideas, feedback, and contributions are welcome.

## Install

Coolship needs Go 1.26 or newer, matching `coolify-cli` so code can move upstream. Newer toolchains build it unchanged.

```bash
git clone https://github.com/joaomnuno/coolship.git
cd coolship
go build -o coolship .
```

Inside this repository, use `./scripts/go` instead of `go` so build and test caches stay in `.cache/` rather than your home directory.

## Getting started

Coolship reuses the contexts that [`coolify-cli`](https://github.com/coollabsio/coolify-cli) already stores, so if you have authenticated there, you are ready to go.

```bash
cd my-app
coolship link
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

### `coolship link`

Bind the repository to an **existing** Coolify application and write `coolship.toml`. Linking only changes local configuration; it never creates remote resources.

Supply selectors to run without prompts, which is what CI needs:

```bash
coolship link --project Personal --environment production --application fenix-bot
```

Names are matched exactly within their selected parent. Use `--project-uuid`, `--environment-uuid`, or `--application-uuid` to pin identity instead of a name; `link` writes a pin automatically when a name cannot describe the selection uniquely.

Replacing a different existing binding requires confirmation, or `--replace` when noninteractive. Re-running `link` with the same result leaves the file, including its comments, untouched.

### `coolship status`

Report the linked application's current status and URL.

```bash
coolship status
```

### `coolship deploy`

Deploy the linked application using the source and branch **already configured in Coolify**. This does not upload your worktree or push local commits.

```bash
coolship deploy                  # wait for the deployment to finish
coolship deploy --no-wait        # return the queued deployment UUID
coolship deploy --force          # rebuild without cache
coolship deploy --timeout 20m
```

Coolship submits one deployment and then observes exactly the UUID that submission returned, never "the latest deployment", which could belong to someone else. While waiting, the server's build log streams to stderr — clone, build, rolling update, health checks — when your token may read it; otherwise progress continues without it. Interrupting the command stops local waiting only; the remote deployment continues, and the UUID is reported so you can pick it back up.

```text
$ coolship deploy
Deployment 03dusayin5rleswixblvdqba: queued
Deployment 03dusayin5rleswixblvdqba: in_progress
Starting deployment of joaomnuno/example-coolify-project:main to Master Ubuntu.
Building docker image started.
Building docker image completed.
Rolling update started.
Attempt 2 of 10 | Healthcheck status: "healthy"
Rolling update completed.
Deployment 03dusayin5rleswixblvdqba: finished
Deployment: 03dusayin5rleswixblvdqba
Application: coolship-example (mm4c0zpbrzx8z96t0qiw3tff)
Status: finished
```

### `coolship logs`

Read runtime logs from the linked application.

```bash
coolship logs
coolship logs --lines 500
coolship logs --follow
```

### `coolship open`

Open the application's public URL — or, with `--dashboard`, its page in Coolify — in your default browser. The URL is always printed on stdout, and nothing is launched when stdin is not a terminal or with `--print`, so it composes with other tools.

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

Delete `coolship.toml`. Nothing on the server changes. Deletion asks for confirmation, or requires `--yes` when noninteractive, and refuses if the file changed since it was read.

### `coolship preview`

Deploy the preview Coolify holds for a pull request, and observe it exactly like `deploy`.

```bash
coolship preview --pr 42
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

A command after `--` runs directly; the configured `dev` string runs through your shell. Unlike `env pull`, `dev` injects shared references as the values they resolve to. Withheld values are reported and left to your own environment. The command's exit status becomes Coolship's, and Ctrl-C is forwarded to it.

### `coolship env`

Synchronize a local dotenv file with the linked application's variables. The file defaults to `.env` in the application root and is created with private permissions.

```bash
coolship env pull                 # remote → .env, keeping local-only keys and comments
coolship env diff                 # what push would change, values masked
coolship env diff --show-values
coolship env push                 # create and update; asks first
coolship env push --prune --yes   # also delete remote-only keys, without asking
```

Three rules keep this safe:

* **Two scopes.** Coolify keeps a separate copy of every variable for preview deployments. Commands act on the regular scope unless `--preview` is given, and never touch the other one — though Coolify itself creates a preview copy of any regular variable that `push` creates, so a later `--preview` diff will show them.
* **Withheld values are never invented.** A value Coolify hides (shown-once secrets) is noted in the file as a comment rather than written empty, is reported as `?` in a diff, and is overwritten by `push` only with `--force`.
* **References stay references.** A shared variable such as `{{team.API_KEY}}` is pulled and compared as that reference, never as the value it resolves to, so a push cannot replace the reference with the secret.

`push` preserves each variable's literal, multiline, and shown-once flags — the server resets them when an update omits them. Changes take effect on the next deployment.

### Shared options

| Flag | Purpose |
| --- | --- |
| `--cwd` | Act on this directory without changing the process directory. |
| `--config` | Use an explicit project configuration path. |
| `--context` | Use this Coolify CLI instance for this invocation. |
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

Coolship does not store credentials or ask you to authenticate twice. It reads the Coolify CLI configuration:

* Unix and macOS: `~/.config/coolify/config.json`
* Windows: `%APPDATA%\coolify\config.json`

The instance is selected by `--context`, then the committed `project.context`, then the single default instance. A missing or ambiguous choice is an error rather than a guess.

For CI, where Coolify CLI may not be installed, supply the pair:

```bash
export COOLSHIP_URL=https://coolify.example.com
export COOLSHIP_TOKEN=…
coolship deploy
```

Both are required together, and the pair cannot be combined with `--context` or `--coolify-config`. It overrides any committed context for that invocation, and Coolship says so in a warning.

## Output and exit codes

Results go to stdout; prompts, progress, and diagnostics go to stderr, so piping stdout stays useful. `--format json` prints one result object, and `logs --format json` prints newline-delimited events.

| Code | Meaning |
| --- | --- |
| `0` | Success. |
| `1` | The operation failed. |
| `2` | Invalid input, configuration, or selection. |
| `130` | Interrupted. |

## Server compatibility

**Verified against Coolify 4.3.18.** Every command was run end to end against a live instance: linking, deploying, and following logs of a real Dockerfile application, syncing its variables in both scopes, deploying a webhook-created pull request preview, running a local process with its variables, and linking a two-target monorepo. The 4.3.19 source has no changes to any endpoint Coolship uses, so it is expected to behave identically; other versions are untested.

Limits worth knowing:

* Deployment states are interpreted as `queued`, `in_progress`, `finished`, `failed`, and `cancelled-by-user`. An unknown state is shown as-is and waits for the timeout instead of being guessed.
* `logs --follow` polls snapshots and compares overlapping lines. The endpoint has no cursor, so log rotation or a container restart can cause gaps or duplicates. Coolship reports the reset rather than pretending the stream is lossless.
* Selecting a container is not supported, because the server ignores the parameter the Coolify CLI sends for it.
* Build logs and secret values can be withheld by token ability and team role. Withheld data is reported as unavailable, never as empty data.
* An uncertain deployment submission is never retried automatically, since the API defines no idempotency key. Coolship reports what it knows so you can recover manually.

[ARCHITECTURE.md](ARCHITECTURE.md) records the verification and the server behaviors it uncovered.

## Planned

Every command from the original brief is implemented. Remaining directions are tracked in [ROADMAP.md](ROADMAP.md): `init` for scaffolding new applications, and sharing packages with `coolify-cli` once their interfaces settle.

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
