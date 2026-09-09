# Coolship

> A project-local developer CLI for Coolify.

Coolship aims to bring a **Wrangler-like developer experience** to [Coolify](https://coolify.io/), focused on the workflow between your local project and its deployed application.

Instead of repeatedly dealing with application UUIDs, projects, environments, and dashboard navigation, Coolship links a local repository to a Coolify application and lets you work with it directly from the terminal.

```bash
coolship link
coolship status
coolship deploy
coolship logs
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

🚧 **Early development.** The first milestone is implemented and tested: `link`, `status`, `deploy`, and `logs`. Nothing has yet been run against a live Coolify server — see [Server compatibility](#server-compatibility) before relying on it.

Ideas, feedback, and contributions are welcome.

## Install

Coolship needs Go 1.26 or newer.

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

Coolship submits one deployment and then observes exactly the UUID that submission returned, never "the latest deployment", which could belong to someone else. Interrupting the command stops local waiting only; the remote deployment continues, and the UUID is reported so you can pick it back up.

### `coolship logs`

Read runtime logs from the linked application.

```bash
coolship logs
coolship logs --lines 500
coolship logs --follow
```

### Shared options

| Flag | Purpose |
| --- | --- |
| `--cwd` | Act on this directory without changing the process directory. |
| `--config` | Use an explicit project configuration path. |
| `--context` | Use this Coolify CLI instance for this invocation. |
| `--coolify-config` | Read credentials from an explicit Coolify CLI configuration file. |
| `-e`, `--environment` | Override the remote environment for this invocation. |
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

Coolship has been developed against the Coolify server **source**, and is tested against a controlled HTTP server in this repository. It has not yet been run against a live instance, so there is no verified supported version range. Concretely:

* Deployment states are interpreted as `queued`, `in_progress`, `finished`, `failed`, and `cancelled-by-user`. An unknown state is shown as-is and waits for the timeout instead of being guessed.
* `logs --follow` polls snapshots and compares overlapping lines. The endpoint has no cursor, so log rotation or a container restart can cause gaps or duplicates. Coolship reports the reset rather than pretending the stream is lossless.
* Selecting a container is not supported, because the inspected server ignores the parameter the Coolify CLI sends for it.
* Build logs and secret values can be withheld by token ability and team role. Withheld data is reported as unavailable, never as empty data.
* An uncertain deployment submission is never retried automatically, since the API defines no idempotency key. Coolship reports what it knows so you can recover manually.

[ARCHITECTURE.md](ARCHITECTURE.md) records these limits and the source observations behind them.

## Planned

None of the following exist yet; they are the directions the architecture is built to accommodate. See [ROADMAP.md](ROADMAP.md).

```text
coolship open
coolship unlink
coolship config
coolship doctor

coolship env pull
coolship env push
coolship env diff

coolship dev
coolship preview
```

Monorepo support is also planned. The configuration and discovery layers already keep the project, the selected target, and the application root as separate concepts, so named targets can be added without restructuring commands.

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
