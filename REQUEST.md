# Coolship — Engineering Architecture Brief

## Context

I want to build a developer-focused CLI for Coolify inspired by Cloudflare's Wrangler.

The important distinction is that this is **not intended to be another general-purpose Coolify administration CLI**.

The existing Coolify CLI already covers that role.

The goal of Coolship is instead to provide a **project-local developer experience** around Coolify.

A developer should be able to enter a repository and run commands such as:

```bash
coolship link
coolship deploy
coolship logs
coolship env pull
coolship open
```

without repeatedly specifying application UUIDs, project IDs, environment IDs, or other Coolify infrastructure details.

The core idea is:

> `coolify-cli` manages Coolify resources.
> Coolship manages the relationship between the current local project and those Coolify resources.

Long-term, I would like the architecture to make it realistic for parts of Coolship to eventually be upstreamed or integrated into the official Coolify CLI.

For that reason, this project should be written in **Go** and should intentionally stay reasonably compatible with the architectural conventions used by `coolify-cli`.

---

# Reference repositories

There are two important local repositories that should be studied before deciding the structure of this project.

## Coolify CLI

```text
/data/projects/coolify-cli
```

This is the most important architectural reference.

Please inspect:

* repository layout
* Cobra command organization
* configuration handling
* Coolify context/authentication handling
* API client organization
* services
* models
* output/terminal helpers
* error handling
* command dependency wiring
* how commands resolve Coolify resources
* how much of the implementation lives in `internal/`
* conventions that would make future code movement between Coolship and `coolify-cli` easier

Do **not** simply fork the structure blindly.

The purpose is to understand how the official CLI is organized so Coolship can remain compatible with it where that makes sense.

---

## Cloudflare Workers SDK / Wrangler

Wrangler is available locally here:

```text
/data/projects/workers-sdk
```

Find the Wrangler package inside this monorepo and study it as the primary **developer-experience reference**.

The goal is not to port Wrangler or reproduce its implementation.

Instead, examine how Wrangler structures concepts such as:

* project-local configuration
* command hierarchy
* command context
* environment selection
* local project discovery
* deployment workflows
* interactive linking/setup
* configuration resolution
* local vs remote state
* development workflows
* logs / tail functionality
* environment/secrets workflows
* command aliases and ergonomics
* project initialization
* reusable command infrastructure
* separation between CLI presentation and lower-level APIs

Wrangler is useful because it represents the kind of experience Coolship is trying to provide:

```text
current directory
      ↓
project configuration
      ↓
known remote application
      ↓
simple developer commands
```

rather than requiring users to think directly in terms of API resources.

---

# Primary task

The first goal is **not to implement every command**.

The first goal is to establish a strong project architecture.

Please study both reference codebases and then design Coolship so that its responsibilities are clearly separated.

The structure should support:

1. project discovery
2. project-local configuration
3. Coolify authentication/context reuse
4. resolving a local project to Coolify resources
5. command execution
6. Coolify API access
7. developer-oriented terminal output
8. future monorepo support
9. future extension into preview/dev workflows
10. possible future upstreaming into `coolify-cli`

The architecture matters more at this stage than completing all functionality.

---

# Core architectural concept

Coolship should revolve around a **project context**.

For example:

```text
filesystem
   │
   ▼
find project root
   │
   ▼
load coolship config
   │
   ▼
resolve Coolify context
   │
   ▼
resolve project/environment/application
   │
   ▼
ProjectContext
   │
   ├── deploy
   ├── logs
   ├── status
   ├── env
   └── open
```

Commands should generally operate on this resolved context rather than independently resolving resources themselves.

For example, this:

```bash
coolship deploy
```

should conceptually be:

```text
discover project
→ load config
→ resolve application
→ deploy application
```

The `deploy` command should not contain all of that logic itself.

---

# Suggested conceptual layers

The exact directory names can change after inspecting the reference repositories, but I expect the architecture to contain roughly these responsibilities.

## CLI layer

Responsible for:

* Cobra commands
* flags
* arguments
* interactive prompts
* command help
* user-facing output
* calling application/domain services

It should contain very little Coolify-specific business logic.

Possible structure:

```text
cmd/
  root.go
  init.go
  link.go
  deploy.go
  logs.go
  status.go
  open.go

  env/
    env.go
    pull.go
    push.go
    diff.go
```

Follow `coolify-cli` conventions where appropriate.

---

# Project layer

This is probably the most important Coolship-specific component.

Something conceptually like:

```text
internal/project/
```

Responsibilities:

* find project root
* locate Coolship configuration
* load configuration
* validate configuration
* determine selected application
* support future monorepo project selection
* expose a resolved project context

For example:

```go
type Project struct {
    Root   string
    Config Config
}
```

and eventually perhaps:

```go
type Context struct {
    Project     Project
    Coolify     CoolifyContext
    Application ApplicationReference
}
```

Exact types are not important yet.

What matters is establishing a clean abstraction around:

> "What Coolify resource does the current directory represent?"

---

# Configuration layer

We will probably use something along the lines of:

```text
coolship.toml
```

Example:

```toml
[project]
context = "home"
project = "personal"
environment = "production"
application = "fenix-bot"
```

This format is not final.

Please look at both:

```text
/data/projects/coolify-cli
```

and Wrangler's configuration architecture inside:

```text
/data/projects/workers-sdk
```

before defining this.

Consider separating:

### Committed project configuration

```text
coolship.toml
```

from:

### Local resolved state/cache

```text
.coolship/
```

For example:

```text
.coolship/state.json
```

The committed configuration should describe the project semantically.

Local state may cache resolved UUIDs or other machine-specific information.

Secrets should never be stored in the repository configuration.

---

# Coolify configuration compatibility

One important goal is to avoid creating a completely separate authentication system if possible.

Study how:

```text
/data/projects/coolify-cli
```

stores and resolves:

* contexts
* API URLs
* tokens
* selected/default context

Ideally Coolship can reuse or remain compatible with the user's existing Coolify CLI contexts.

Conceptually:

```text
                 Coolify
                    ▲
                    │
             Coolify API client
                    ▲
             ┌──────┴──────┐
             │             │
       coolify-cli      Coolship
```

Users should ideally not need to authenticate twice.

However, avoid tightly coupling Coolship to implementation details that make it impossible to run independently.

---

# Coolify API layer

There should be a clear boundary around Coolify communication.

For example:

```text
internal/coolify/
```

or another structure informed by `coolify-cli`.

It should expose operations conceptually like:

```text
Applications
Deployments
Environments
Projects
EnvironmentVariables
Logs
```

Commands should not construct arbitrary HTTP requests directly.

Likewise, project discovery should not know how HTTP calls work.

We want something approximately like:

```text
cmd
 ↓
project/application services
 ↓
Coolify client
 ↓
Coolify API
```

Study the official CLI first because there may already be abstractions worth mirroring.

---

# Resource resolution

This deserves its own abstraction.

A project configuration may contain:

```toml
project = "Personal"
environment = "production"
application = "fenix-bot"
```

but the API may operate on UUIDs.

Avoid scattering code like:

```text
find project
find environment
find application
get UUID
```

through every command.

Instead, create a centralized resolver concept.

Something roughly like:

```text
Resolver
  ├── ResolveProject(...)
  ├── ResolveEnvironment(...)
  └── ResolveApplication(...)
```

or:

```text
ResolveProjectContext(...)
```

The exact API should emerge from the design.

The important thing is that:

```bash
coolship deploy
coolship logs
coolship env pull
```

all share the same resolution mechanism.

---

# Application/service layer

Consider having application-level operations separate from both Cobra and raw Coolify API calls.

For example:

```text
internal/deploy/
internal/logs/
internal/environment/
```

or:

```text
internal/service/
```

depending on what fits the conventions of the existing Coolify CLI.

For example:

```text
DeployCurrentProject()
TailCurrentProjectLogs()
PullEnvironment()
```

would orchestrate:

```text
project context
+
Coolify API
+
output/progress
```

without putting that orchestration directly inside Cobra handlers.

---

# Terminal / UI layer

Coolship should eventually have a polished developer experience.

However, do not spend significant time building fancy output yet.

The structure should simply make it possible later.

We should eventually support things like:

```text
$ coolship deploy

→ fenix-bot
→ production

✓ Deployment queued

  cloning
  building
  deploying
  health check

✓ Deployed

https://fenix.example.com
```

Study how both Wrangler and `coolify-cli` handle:

* stdout/stderr
* spinners
* interactive prompts
* tables
* machine-readable output
* colors
* verbosity
* errors

Avoid embedding formatting logic deeply into domain code.

---

# Initial command structure

Design the CLI around something approximately like:

```text
coolship
├── init
├── link
├── unlink
│
├── deploy
├── logs
├── status
├── open
│
├── env
│   ├── pull
│   ├── push
│   └── diff
│
├── dev
├── preview
│
├── config
└── doctor
```

Not all of these need to be implemented now.

The command hierarchy should simply make them natural to add later.

---

# MVP commands

The first implementation milestone should probably only establish:

```bash
coolship link
coolship status
coolship deploy
coolship logs
```

Potentially:

```bash
coolship open
```

as well.

The purpose of these commands is to validate the architecture.

For example, if implementing `logs` requires duplicating application-resolution logic from `deploy`, that is a sign the architecture needs improvement.

---

# `link` as an important architectural flow

`coolship link` is central to the project.

Conceptually:

```text
detect repository root

       ↓

select Coolify context

       ↓

select project

       ↓

select environment

       ↓

select application

       ↓

write project configuration
```

Example UX:

```text
$ coolship link

Coolify context:
> home

Project:
> Personal

Environment:
> production

Application:
> fenix-bot

✓ Linked this repository to Personal / production / fenix-bot
```

Please pay particular attention to how this flow should be structured.

The interactive prompt itself is not the hard part.

The reusable resource-selection and project-binding architecture is.

---

# Monorepos

Do not fully implement monorepos yet, but avoid an architecture that prevents them.

We may eventually want:

```toml
[apps.web]
application = "frontend"
root = "apps/web"

[apps.api]
application = "backend"
root = "apps/api"
```

Then:

```bash
coolship deploy web
coolship logs api
```

or:

```bash
cd apps/web
coolship deploy
```

should be possible.

Project discovery should therefore not permanently assume:

```text
one repository = one Coolify application
```

even if that is what the first version supports.

---

# Future `dev` command

Do not implement a complicated local runtime yet.

Just keep the architecture open to eventually supporting:

```bash
coolship dev
```

which might:

```text
resolve project
↓
resolve development environment
↓
fetch environment variables
↓
inject them into a process
↓
run configured local command
```

Wrangler's development command should be studied here for architectural inspiration.

Again, we are interested primarily in how Wrangler separates:

* configuration
* runtime
* bindings
* process execution
* project context

rather than copying Wrangler's Workers-specific behavior.

---

# Future preview deployments

Also leave room for something like:

```bash
coolship preview
```

which could later understand:

```text
current branch
Git repository
Pull Request
Coolify preview deployment
```

This does not need implementation now.

It is simply another reason not to design the project context too narrowly.

---

# Environment variables

The eventual design should support:

```bash
coolship env pull
coolship env push
coolship env diff
```

Environment-variable behavior can become complicated because we may need to distinguish:

```text
local environment
runtime variables
build variables
preview variables
secrets
```

Please inspect how both Coolify CLI and Wrangler structure environment/secrets functionality.

Again, the immediate deliverable is architecture, not a complete synchronization engine.

---

# Relationship to `coolify-cli`

The architecture should intentionally allow for a future where Coolship functionality is moved into:

```text
/data/projects/coolify-cli
```

For example, today's:

```bash
coolship link
coolship deploy
coolship logs
```

might eventually become:

```bash
coolify link
coolify deploy
coolify logs
```

where the official CLI becomes aware of project-local configuration.

This means we should avoid unnecessary Coolship-specific coupling in core modules.

A useful mental model is:

```text
             ┌─────────────────────┐
             │ Project-local logic │
             └──────────┬──────────┘
                        │
             ┌──────────▼──────────┐
             │ Coolify operations  │
             └──────────┬──────────┘
                        │
                   Coolify API
```

The first block is where Coolship provides most of its unique value.

Ideally, that block could eventually be moved or adapted upstream.

---

# Do not fork `coolify-cli` as the starting architecture

The repositories can share conventions, ideas and potentially authentication/configuration compatibility.

However, Coolship should initially remain its own Go module.

Do not build Coolship by directly modifying:

```text
/data/projects/coolify-cli
```

The existing repository is a **reference and potential future upstream target**.

Coolship needs enough independence to experiment with UX and project-local concepts without being constrained by every existing assumption in the administrative CLI.

---

# Things I specifically want you to investigate

Before settling on the directory structure, inspect both reference repositories and determine:

### From `coolify-cli`

* How commands are registered.
* How Cobra is structured.
* How context/configuration is loaded.
* How API clients are instantiated.
* How services are structured.
* How models are organized.
* How terminal output is abstracted.
* What code is inaccessible because it lives under `internal`.
* Whether configuration compatibility can reasonably be maintained.
* Which architectural conventions would be useful to copy.

### From Wrangler

* How a command discovers its project.
* How configuration files are found and loaded.
* How environment selection works.
* How command execution gets a shared project context.
* How deployment-related commands are separated.
* How `dev` is architecturally separated from deployment.
* How log/tail functionality is structured.
* How secrets/environment commands are grouped.
* How Wrangler handles monorepos or working-directory overrides.
* How reusable infrastructure is separated from individual commands.

---

# Initial architecture deliverable

Before implementing significant functionality, I would like a proposed tree similar in level of detail to:

```text
coolship/
├── cmd/
│   ├── root.go
│   ├── link.go
│   ├── deploy.go
│   ├── logs.go
│   ├── status.go
│   └── env/
│       ├── env.go
│       ├── pull.go
│       ├── push.go
│       └── diff.go
│
├── internal/
│   ├── config/
│   ├── project/
│   ├── coolify/
│   ├── resolver/
│   ├── service/
│   └── ui/
│
├── main.go
├── go.mod
└── README.md
```

This is **only an example**.

Please change it if the structures used by `coolify-cli` or Wrangler suggest a cleaner approach.

For each important package, document its responsibility and allowed dependencies.

For example:

```text
cmd
  may depend on project/service/ui
  should not make raw HTTP requests

project
  may depend on config
  should not depend on Cobra

coolify
  owns communication with the Coolify API
  should not care about the current working directory

resolver
  maps semantic project references to Coolify resources
  should not contain terminal UI

ui
  owns terminal presentation
  should not contain Coolify business logic
```

The dependency direction is more important than the exact names.

---

# Avoid premature implementation

At this stage, please avoid spending significant effort on:

* elaborate spinners
* highly polished deployment output
* full environment synchronization
* preview deployment automation
* local proxying
* Docker orchestration
* complicated `dev` behavior
* automatic GitHub integration
* every Coolify API endpoint
* generic Coolify server administration

Those may come later.

The initial implementation should prove that the architecture supports the intended workflow cleanly.

---

# Success criteria

The initial structure is successful if implementing these commands:

```bash
coolship link
coolship status
coolship deploy
coolship logs
```

does **not** require duplicating:

* project discovery
* Coolify authentication
* application lookup
* configuration loading
* context selection
* API client initialization

and if adding:

```bash
coolship env pull
```

later feels like extending the architecture rather than restructuring the entire CLI.

The project should also feel sufficiently idiomatic to Go and sufficiently close to the patterns used by `coolify-cli` that upstreaming parts of it later would be realistic.

---

# Guiding principle

The main design question throughout the project should be:

> How do we make the current repository itself act as the context for Coolify commands?

Coolship should turn this:

```text
local repository
+
Coolify project
+
environment
+
application UUID
+
deployment API
```

into this:

```bash
coolship deploy
```

Wrangler is the developer-experience reference.

`coolify-cli` is the compatibility and potential-upstream reference.

Build the architecture around those two ideas before worrying about feature completeness.
