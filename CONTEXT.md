# Coolship

A project-local developer CLI for Coolify. It binds a repository to one Coolify application and runs the daily verbs against that binding, so the developer never types a UUID.

## Language

**Terminal experience**:
Everything Coolship prints or asks in a terminal: prompts, progress, results, and errors. It is the whole of Coolship's user interface; there is no graphical one.
_Avoid_: UI tool, GUI, output layer

**Context**:
A saved Coolify instance (URL and token) in the Coolify CLI configuration. A token belongs to one team on that instance, so the team is a property of the context, never a choice.
_Avoid_: Team, account, server (a server is a Coolify host that runs applications)

**Target**:
The project, environment, and application a directory is bound to. A monorepo names several targets; a plain repository has one.
_Avoid_: Binding (the file that records a target), app, resource

## Files

**Configuration**:
The project's `coolship.toml`: which target a directory is bound to, and the facts the whole team shares. Committed.
_Avoid_: Settings, project config, binding file

**Preferences**:
One developer's tastes on one machine: default verbosity, whether build logs stream, color. Never committed and never a project fact.
_Avoid_: User config, settings, options

**Credentials**:
The Coolify CLI file that holds each context's URL and token. Coolship reads and writes the same file so one login serves both tools.
_Avoid_: Coolify config, auth file, config.json

## Terminal experience

**Verbosity**:
How much a run shows: normal (names only), verbose (UUIDs, timings, one line per request), or debug (full requests and responses with the token masked).
_Avoid_: Advanced mode, log level, developer mode

**Stage**:
One step of a deployment that Coolify names in its build log: image build, rolling update, container start, health check.
_Avoid_: Phase, step, progress

**Menu**:
The screen `coolship ui` opens: the target, its status, and its last deployment, then the verbs to run. It is a way to reach the verbs, not a place where any of them run differently.
_Avoid_: TUI, dashboard, interactive mode
