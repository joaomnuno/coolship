# Coolship

> A project-local developer CLI for Coolify.

Coolship aims to bring a **Wrangler-like developer experience** to [Coolify](https://coolify.io/), focused on the workflow between your local project and its deployed application.

Instead of repeatedly dealing with application UUIDs, projects, environments, and dashboard navigation, Coolship links a local repository to a Coolify application and lets you work with it directly from the terminal.

```bash
coolship link
coolship deploy
coolship logs
coolship env pull
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

## Vision

A typical workflow might look something like this:

```bash
git clone git@github.com:example/my-app.git
cd my-app

coolship link

coolship env pull
coolship deploy
coolship logs -f
```

After linking a repository, Coolship should have enough context to make most commands work without requiring resource IDs or repeated configuration.

## Planned commands

The exact interface is still being explored, but the current direction includes:

```text
coolship link
coolship unlink

coolship deploy
coolship logs
coolship status
coolship open

coolship env pull
coolship env push
coolship env diff

coolship dev
coolship preview
```

### `coolship link`

Associate the current repository with an existing Coolify application.

```bash
coolship link
```

Potentially selecting:

```text
Coolify instance
└── Project
    └── Environment
        └── Application
```

The resulting project configuration could then be reused by every other command.

### `coolship deploy`

Deploy the application linked to the current project.

```bash
coolship deploy
```

Ideally with a developer-oriented deployment experience showing build and deployment progress directly in the terminal.

### `coolship logs`

View application logs without looking up its UUID.

```bash
coolship logs
coolship logs -f
```

### `coolship env`

Synchronize local and remote environment variables.

```bash
coolship env pull
coolship env push
coolship env diff
```

### `coolship open`

Open the current application or its Coolify dashboard.

```bash
coolship open
coolship open dashboard
```

## Project configuration

Coolship will likely use a repository-local configuration file such as:

```text
coolship.toml
```

For example:

```toml
[project]
context = "home"
project = "personal"
environment = "production"
application = "my-app"
```

Credentials and sensitive information should **not** be stored in the project configuration.

Where possible, Coolship should remain compatible with the existing Coolify CLI configuration and contexts rather than requiring users to authenticate twice.

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

## Status

🚧 **Early development**

The project is currently exploring the command structure, project configuration format, Coolify API integration, and how closely it should integrate with `coolify-cli`.

Ideas, feedback, and contributions are welcome.

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
