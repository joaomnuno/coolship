# Coolship architecture

This document is based on [README.md](README.md) and the complete [engineering brief](REQUEST.md).

Milestone 1 is implemented: the module, `link`, `status`, `deploy`, and `logs`, with the packages and boundaries described below. Sections that describe later work are marked, and [ROADMAP.md](ROADMAP.md) tracks status. Where this document describes behavior that is not yet built, it states a decision to follow, not an existing capability.

The central decision is to resolve the current directory into one explicit `project.Context`, then reuse that resolution pipeline for every application workflow. Cobra owns the interaction. Application services own the workflow. A small Coolify adapter owns HTTP communication.

## 1. What the reference codebases establish

The following local checkouts were inspected. These revisions identify the source observations, not a promise of compatibility with every released version. The inspected source paths had no tracked modifications. Source links below point into the sibling local checkouts.

| Reference | Inspected revision | Role |
| --- | --- | --- |
| `/data/projects/coolify-cli` | `76ca47187a0c34b6f68c5c44b192fc457749e2c7` | Go conventions, configuration compatibility, potential upstream target. |
| `/data/projects/workers-sdk` | `36aed7f0f2db5056af9df917cf6c22a2be950b1e` | Wrangler project discovery, command infrastructure, and developer workflows. |
| `/data/projects/coolify` | `424dbd36fff39a9ccd22efbee019cf490dcf9fc8` | Server routes and behavior that constrain the proposed client. |

### Coolify CLI: conventions to keep and boundaries to improve

- **Command organization:** [cmd/root.go](../coolify-cli/cmd/root.go) registers Cobra trees through `New…Command()` constructors. [cmd/application/application.go](../coolify-cli/cmd/application/application.go) groups application commands, aliases, and nested environment commands. Keep these constructor and directory conventions. Use explicit dependency arguments instead of package globals and `init()` registration.
- **Configuration and authentication:** [internal/config/config.go](../coolify-cli/internal/config/config.go), [instance.go](../coolify-cli/internal/config/instance.go), and [internal/cli/client.go](../coolify-cli/internal/cli/client.go) establish the JSON format, default instance, context selection, and token override. The root also initializes Viper, while the client helper loads typed configuration directly. Coolship should read the compatible format through one typed adapter, without inheriting both loading paths.
- **Client and services:** [internal/api/client.go](../coolify-cli/internal/api/client.go) supplies bearer authentication, `/api/v1/`, context-aware requests, timeouts, and retries. [internal/service/project.go](../coolify-cli/internal/service/project.go) and [deployment.go](../coolify-cli/internal/service/deployment.go) wrap resource endpoints. Keep constructor injection and context propagation. Initially place transport and endpoint wrappers together in `internal/coolify`; reserve Coolship's `internal/service` for project workflows.
- **Models and errors:** [internal/models/project.go](../coolify-cli/internal/models/project.go) holds response types. [internal/api/error.go](../coolify-cli/internal/api/error.go) provides typed HTTP errors; callers wrap errors with `%w`. Keep these conventions, but keep display tags and formatting out of core models.
- **Resource lookup:** [cmd/deployment/name.go](../coolify-cli/cmd/deployment/name.go) lists resources and selects the first exact name match. Most application commands take UUIDs. Coolship needs hierarchy-scoped resolution and explicit ambiguity errors, shared by all commands.
- **Output:** [internal/output/formatter.go](../coolify-cli/internal/output/formatter.go) abstracts table, JSON, and pretty output with an injectable writer. Some commands print directly, and deployment services also format logs. Keep writer injection and machine output; place all terminal formatting in `ui`. Do not copy direct command printing or service formatting.
- **Long-running operations:** [cmd/application/logs.go](../coolify-cli/cmd/application/logs.go) polls snapshots every two seconds and manages signals inside Cobra. Coolship should put polling in services and cancellation at the process boundary.
- **Environment variables:** [cmd/application/env/sync.go](../coolify-cli/cmd/application/env/sync.go) combines file parsing, comparison, API mutation, and progress output inside a handler. Preserve the API's variable attributes from [internal/models/application.go](../coolify-cli/internal/models/application.go), but move synchronization policy into an application service when that feature is built.

The useful configuration, API, models, services, and output packages are under Go's `internal/` boundary. A separate Coolship module cannot import them directly. Do not bypass that boundary with a module name trick, `replace`, a fork, or execution of the `coolify` binary. Compatibility is initially a tested file-format contract plus a small independent API adapter.

### Wrangler: patterns to adapt

Wrangler is in `packages/wrangler`, with reusable infrastructure also extracted into sibling packages. It does not provide one universal, fully resolved remote project context; Coolship's context is a design derived from the brief.

| Concern | Observed source and behavior | Coolship decision |
| --- | --- | --- |
| Command infrastructure | [core/create-command.ts](../workers-sdk/packages/wrangler/src/core/create-command.ts), [core/register-yargs-command.ts](../workers-sdk/packages/wrangler/src/core/register-yargs-command.ts), and [core/types.ts](../workers-sdk/packages/wrangler/src/core/types.ts) define commands and provide shared configuration, SDK access, logging, prompts, and error handling. | Use Cobra constructors and one shared service preparation path. Keep terminal helpers out of the resolved project context. |
| Discovery and configuration | [workers-utils/config/config-helpers.ts](../workers-sdk/packages/workers-utils/src/config/config-helpers.ts) handles explicit paths, upward discovery, and optional generated-config redirects. [wrangler/config/index.ts](../workers-sdk/packages/wrangler/src/config/index.ts) delegates parsing and normalization to shared utilities. | Separate finding a file from parsing and validating its contents. Start with TOML only; do not add generated-config redirection. |
| Environment selection | [workers-utils/config/validation.ts](../workers-sdk/packages/workers-utils/src/config/validation.ts) resolves `--env` or `CLOUDFLARE_ENV` and distinguishes inheritable fields from environment-specific fields. | Define explicit precedence and fail on missing Coolify environments. Do not copy Workers inheritance rules or silently synthesize remote environments. |
| Working directory and aliases | [wrangler/index.ts](../workers-sdk/packages/wrangler/src/index.ts) registers the hierarchy and aliases, supports `--config` and `--env`, and changes the process directory for `--cwd`. | Support working-directory and configuration overrides, but pass absolute paths instead of using `os.Chdir`. Keep a small command vocabulary. |
| Setup and linking | [init.ts](../workers-sdk/packages/wrangler/src/init.ts) can delegate scaffolding to Create Cloudflare or import a Worker; [deploy/autoconfig.ts](../workers-sdk/packages/wrangler/src/deploy/autoconfig.ts) handles setup during deployment. | Keep project binding reusable and explicit. `link` selects an existing application; deployment does not silently create resources. |
| Deployment | [deploy/index.ts](../workers-sdk/packages/wrangler/src/deploy/index.ts) delegates work to bundle infrastructure and `@cloudflare/deploy-helpers`. | Keep a thin command handler and an independently testable deployment service. Coolify performs the build and deployment. |
| Local development | [dev/start-dev.ts](../workers-sdk/packages/wrangler/src/dev/start-dev.ts) and [api/startDevWorker/DevEnv.ts](../workers-sdk/packages/wrangler/src/api/startDevWorker/DevEnv.ts) separate configuration, bundling, local/remote runtimes, and proxy controllers. | Add process execution separately when `dev` exists. Do not reproduce Miniflare, controller buses, or local proxies. |
| Logs and tail | [tail/index.ts](../workers-sdk/packages/wrangler/src/tail/index.ts), [createTail.ts](../workers-sdk/packages/wrangler/src/tail/createTail.ts), and [printing.ts](../workers-sdk/packages/wrangler/src/tail/printing.ts) separate command options, WebSocket session setup/cleanup, and formatting. | Separate log acquisition, follow policy, and rendering. Coolify's inspected application endpoint returns snapshots, so do not assume a WebSocket API. |
| Variables and secrets | [secret/index.ts](../workers-sdk/packages/wrangler/src/secret/index.ts) groups remote secret operations; [dev/dev-vars.ts](../workers-sdk/packages/wrangler/src/dev/dev-vars.ts) loads local `.dev.vars`/`.env` values relative to configuration. | Keep local secret files distinct from committed binding configuration. Preserve Coolify's own variable semantics. |
| Local state and presentation | [workers-utils/config-cache.ts](../workers-sdk/packages/workers-utils/src/config-cache.ts) provides file-backed cache infrastructure. [logger.ts](../workers-sdk/packages/wrangler/src/logger.ts) and [dialogs.ts](../workers-sdk/packages/wrangler/src/dialogs.ts) centralize verbosity and interaction. | Make local state disposable, inject streams, and define noninteractive behavior explicitly. Avoid global logger/configuration state and elaborate UI machinery. |

### Server constraints that shape the client

[routes/api.php](../coolify/routes/api.php), [ProjectController.php](../coolify/app/Http/Controllers/Api/ProjectController.php), [ApplicationsController.php](../coolify/app/Http/Controllers/Api/ApplicationsController.php), and [DeployController.php](../coolify/app/Http/Controllers/Api/DeployController.php) support the following conclusions:

- Environment details include applications scoped to a project and environment. Resolution does not require a global application-name search.
- Deployment uses `POST /api/v1/deploy`. The response contains a `deployments` array; HTTP success alone does not establish that the requested application received a deployment UUID.
- Application logs are a JSON object containing a `logs` string. The inspected endpoint accepts `lines` and `show_timestamps`, and has no cursor or streaming contract.
- **The local references differ:** Coolify CLI sends `service_name` for container selection, but this Coolify checkout's application logs handler chooses the first container and does not read that parameter. Defer a container-selection flag until server support is verified.
- Deployment logs may be omitted by server permissions. [ApiSensitiveData.php](../coolify/app/Http/Middleware/ApiSensitiveData.php) requires an appropriate token ability and team administrator/owner status for sensitive data. Missing logs or variable values must not be represented as empty data.

These source observations were then checked against a live Coolify 4.3.18 instance; section 10 records the supported baseline, the sanitized fixtures, and the behavior that only the live server revealed.

## 2. Directory tree

One Go module and one binary are sufficient. Files marked `later` are extension locations, not empty packages or placeholder commands to create now. Related responsibilities share a file until size justifies splitting them; the package boundaries below are the architectural commitment, not the file names inside them.

```text
coolship/
├── main.go                         # Composition, signals, process exit
├── go.mod
├── go.sum
├── scripts/go                      # Go wrapper keeping caches inside the repository
├── cmd/
│   ├── root.go                     # NewRootCommand, shared flags, registration
│   ├── options.go                  # Convert flags into service request values
│   ├── link.go
│   ├── status.go
│   ├── deploy.go
│   ├── logs.go
│   ├── open.go                     # Later
│   ├── init.go                     # Create an application, then bind it
│   ├── unlink.go                   # Later
│   ├── config.go                   # Later
│   ├── doctor.go                   # Later
│   ├── dev.go                      # Later
│   ├── preview.go                  # Later
│   └── env/                        # Later Cobra subpackage
│       ├── env.go
│       ├── pull.go
│       ├── push.go
│       └── diff.go
├── internal/
│   ├── config/
│   │   └── config.go               # Versioned TOML schema, codec, validation
│   ├── project/
│   │   ├── discover.go             # Working directory, config root, Git boundary
│   │   ├── project.go              # Loaded project, selected local target, context
│   │   ├── store.go                # Load and safely write project configuration
│   │   └── state.go                # Later, disposable resolution cache
│   ├── auth/
│   │   ├── credentials.go          # Instance identity and private credentials
│   │   └── coolify_cli.go          # Read existing contexts and select credentials
│   ├── process/
│   │   └── process.go              # Local child process: shell, environment, signals, status
│   ├── gitinfo/
│   │   └── gitinfo.go              # Origin remote and branch through git; remote URL normalization
│   ├── envfile/
│   │   └── envfile.go              # Dotenv codec preserving comments and order
│   ├── models/
│   │   └── models.go               # Resource, deployment, and log identities
│   ├── coolify/
│   │   ├── client.go               # HTTP transport, retries, and client options
│   │   ├── errors.go               # Typed, sanitized HTTP errors
│   │   ├── resources.go            # Projects, environments, applications, deployments, logs
│   │   └── environment.go          # Later variable endpoints
│   ├── resolver/
│   │   ├── resolver.go             # Consumer-owned Catalog, scoped matching, validation
│   │   └── errors.go               # Missing, ambiguous, or changed binding
│   ├── service/
│   │   ├── service.go              # Dependencies, shared preparation, status
│   │   ├── types.go                # Options, public results, ports, events, typed errors
│   │   ├── link.go                 # Resource selection and binding plan/write
│   │   ├── init.go                 # Repository detection, creation plan, then the link write
│   │   ├── deploy.go               # Trigger and observe one deployment
│   │   ├── logs.go                 # Snapshot/follow workflow
│   │   └── environment.go          # Later pull/push/diff policy
│   └── ui/
│       ├── streams.go              # Injected stdin/stdout/stderr, terminal capability
│       ├── prompts.go
│       ├── render.go               # Human and JSON output
│       └── errors.go               # Error presentation and exit classification
├── AGENTS.md
├── ARCHITECTURE.md
├── README.md
├── REQUEST.md
└── ROADMAP.md
```

Tests live next to the owning package, with package-local `testdata/` fixtures where useful. `cmd/flows_test.go` additionally drives the real command tree, services, and HTTP client against an `httptest.Server`, so the composition the executable performs is covered by tests rather than only by `main.go`. There is no separate SDK module, dependency-injection framework, generic repository framework, or public `pkg/` API in this milestone.

## 3. Dependency graph and package purposes

Solid arrows show allowed **direct Go imports**, from importer to imported package. Dashed arrows show runtime calls through interfaces, not imports. Standard-library and third-party imports are omitted. `cmd/env` is a future package.

```mermaid
flowchart TD
    main[main: composition] --> cmd
    main --> service
    main --> project
    main --> auth
    main --> resolver
    main --> coolify
    main --> ui
    main --> gitinfo
    cmd --> envcmd[cmd/env: later]
    cmd --> service
    cmd --> ui
    envcmd --> service
    envcmd --> ui
    ui --> service
    service --> project
    service --> auth
    service --> resolver
    service --> models
    service --> gitinfo
    resolver --> project
    resolver --> models
    project --> config
    project --> models
    coolify --> models
    service -. operation interfaces .-> coolify
    resolver -. Catalog interface .-> coolify
    coolify -. HTTP .-> remote[Coolify API]
```

`service` and `resolver` define the interfaces they consume. `coolify.Client` satisfies those interfaces structurally without importing either package. `main` supplies a client factory, including the conversion from authentication credentials to client constructor arguments. Neither commands nor the resolver create clients.

| Package | Why it exists and what it owns | Allowed direct project imports |
| --- | --- | --- |
| `main` | Makes concrete dependencies visible in one place. Constructs services, adapters, and UI; supplies signal cancellation; executes Cobra and exits once. Constructors do not perform network requests or require a linked project. | `cmd`, `service`, `project`, `auth`, `resolver`, `coolify`, `ui`, `gitinfo`, `process` |
| `cmd` | Translates CLI arguments into typed service requests. Registers commands, conducts prompt interaction, and sends results to UI. Contains no HTTP, filesystem search, credential lookup, or name-matching algorithm. | `service`, `ui`; later `cmd/env` |
| `cmd/env` | Groups future variable commands without making each handler responsible for configuration or authentication. It receives dependencies from its parent and never imports `cmd`. | `service`, `ui` |
| `config` | Defines the committed file contract independently of machine paths and credentials. Parses, validates, normalizes, and encodes TOML. Unknown fields and unsupported schema versions produce actionable errors. | None |
| `project` | Answers which local configuration and application root the current directory selects. Owns discovery, config file I/O, target selection, binding writes, and the context data structure. It never resolves remote names or makes HTTP calls. | `config`, `models` |
| `auth` | Isolates compatibility with Coolify CLI configuration and invocation-only credentials. Owns context-file paths, parsing, selection, and credential validation. It neither modifies the shared context file nor reads project TOML. | None |
| `models` | Gives the resolver, adapter, and workflows a small shared resource vocabulary without importing one another. Owns resource identity and operation data, not credentials, TOML, Cobra, rendering, or polling. | None |
| `coolify` | Encapsulates endpoint paths, request/response conversion, authentication headers, timeouts, HTTP errors, and read retries. It receives a base URL and token; it does not know the current directory or config files. | `models` |
| `resolver` | Converts semantic selectors into a verified project/environment/application binding. Owns hierarchy constraints, exact matching, ambiguity detection, and cached-binding validation policy. Reads resources through `Catalog`; performs no prompts or filesystem writes. | `project`, `models` |
| `service` | Implements project workflows and the one shared preparation path. Owns link planning, deployment observation, log following, and later variable synchronization. Takes ordinary values and `context.Context`; returns results/events/errors. | `project`, `auth`, `resolver`, `models`, `gitinfo` (types and URL normalization only; `git` runs through an injected function) |
| `gitinfo` | Answers where Coolify should clone from: the origin remote, normalized to an https URL, and the checked-out branch. Runs `git` through `os/exec` and nothing else; `main` injects `Inspect` into `service.Dependencies` so tests substitute a function. | None |
| `ui` | Owns prompts, terminal capability checks, colors, progress rendering, JSON, and error presentation. May consume service result/event types; never fetches data or selects a remote target by business rules. | `service` |

The UI dependency on service types is intentional: presentation knows what it renders, while workflows know nothing about presentation. Service event callbacks provide backpressure and return output errors; no global event bus or unbounded background channels are needed.

The `models` package is deliberately small. Keep endpoint-only response envelopes private to `coolify`. Add shared types only when a consumer needs them. Keep transport and endpoint wrappers in one package until their size justifies a split; the split must not reach callers.

## 4. Project context and dependency wiring

The following sketches describe ownership, not finalized signatures or generated scaffolding:

```go
// internal/project
type Project struct {
    ConfigRoot string        // Directory containing coolship.toml.
    ConfigPath string
    GitRoot    string        // Optional; may differ from ConfigRoot.
    Config     config.Config
}

type Target struct {
    Key     string           // "default" initially; later "web" or "api".
    AppRoot string           // Absolute local application root.
    Binding config.Binding  // Semantic selectors and optional UUID pins.
}

type Context struct {
    Project         Project
    Target          Target
    InstanceName    string
    InstanceURL     string
    RemoteProject   models.ProjectRef
    Environment     models.EnvironmentRef
    Application     models.ApplicationRef
}
```

`project.Context` is an explicit, credential-free value, treated as immutable during a command. It contains neither a Cobra command nor an API client. Go's `context.Context` remains a separate cancellation/deadline parameter; it is not a container for project data.

For `status`, `deploy`, `logs`, and later `env pull`, service preparation performs these steps once per invocation:

1. Resolve the effective working directory and config path through `project`.
2. Load and validate TOML; select a local target and apply explicit invocation overrides.
3. Ask `auth` for the selected instance and credentials.
4. Construct one backend through the injected factory.
5. Ask `resolver` to verify the complete remote binding through that backend.
6. Return an internal service session containing `project.Context` and the backend.
7. Execute the requested operation using that session. Polling reuses it without repeating discovery or authentication.

Commands call methods such as `DeployCurrentProject(ctx, request, emit)` or `TailCurrentProjectLogs(ctx, request, emit)`. They do not manually assemble these steps. The exported method names can change without moving responsibilities.

`service/ports.go` should define small interfaces such as deployment submission/inspection and application log reading. `resolver.Catalog` needs only project listing, environment listing/details, and application inspection. A factory may return a composed interface for a session; individual workflows consume only their required subset. Fakes can implement the same contracts.

Configuration-free commands such as help and version must not trigger preparation. `link` uses discovery and an authenticated selection session without requiring an existing complete binding. Future local-only `config` or `unlink` operations use only the local parts they need. Avoid a root `PersistentPreRunE` that requires credentials for every command.

## 5. Configuration, authentication, and local state

### Committed configuration

Keep the README's semantic selectors, add a schema version, and distinguish the local application root:

```toml
version = 1

[project]
context = "home"
project = "Personal"
environment = "production"
application = "fenix-bot"
root = "."
```

`context` names a locally configured Coolify instance. It is not a token or URL. `root` is relative to the configuration directory. Normalize this initial single binding to target key `default`; do not bake a single application into the repository abstraction.

Project, environment, and application strings are names, not strings guessed to be UUIDs. Optional `project_uuid`, `environment_uuid`, and `application_uuid` fields provide explicit identity pins. `link` writes a pin when names cannot uniquely describe a selected resource, or when explicitly requested. Pins contain identifiers, not secrets. When pinned, the UUID is authoritative and a renamed resource is reported as label drift; a missing pinned UUID never falls back to a same-named replacement.

Tokens, environment-variable values, and fetched resource payloads never enter `coolship.toml`. Parse with a typed TOML library; do not merge project TOML and credential JSON into a global Viper store. Pin exact Go and dependency versions during scaffolding rather than freezing them in this design document.

### Discovery and override rules

- `--cwd` resolves against the invocation directory. Pass the resulting absolute directory to services; never change process-global working directory.
- An explicit `--config` resolves against that effective directory and wins over discovery. A missing explicit file is an error except when `link` is deliberately creating it.
- Otherwise search upward for the nearest `coolship.toml`, stopping after examining the nearest Git worktree root, or at the filesystem root outside Git. Recognize both `.git` directories and worktree `.git` files. Do not escape into a parent repository's binding.
- The configuration directory, Git root, and application root are separate values. Config-relative paths never depend on the shell directory used to invoke the command.
- Without configuration, `link` proposes the Git worktree root, or the effective working directory outside Git, as its configuration directory. Other MVP commands report that the project is not linked.
- `--context` overrides the committed context for this invocation. `--environment`/`-e` overrides the selected remote environment and forces fresh application resolution within it. These flags never rewrite the committed file.
- An environment override must agree with any explicit environment or application UUID pin. Reject conflicting overrides with an actionable error instead of retaining an old binding or deploying to a different application silently.

### Compatible credentials

Read the existing Coolify CLI file using its inspected schema:

```json
{
  "instances": [
    {
      "name": "home",
      "fqdn": "https://coolify.example.com",
      "token": "<stored outside the repository>",
      "default": true
    }
  ]
}
```

Mirror the inspected path behavior: Unix/macOS use `~/.config/coolify/config.json`; Windows uses `%APPDATA%\coolify\config.json`, with the CLI's home-based fallback. The inspected Unix path is not generally redirected by `XDG_CONFIG_HOME`; only the CLI's home-lookup failure path uses the XDG fallback. Test this detail rather than assuming generic XDG behavior. Offer `--coolify-config` for an explicit alternate credentials file, separate from project `--config`.

Named context selection is `--context`, then committed `project.context`, then exactly one default instance. Fail on an explicit missing context, duplicate context names, or an absent/ambiguous default. Never switch to another instance after an authentication error. Unknown unrelated JSON fields, including update-check metadata, can be ignored because this adapter is read-only.

For independent CI use, propose `COOLSHIP_URL` plus `COOLSHIP_TOKEN` as an explicit credential pair. The pair supplies the entire invocation's instance without needing Coolify CLI installed. If either is set, require both, and reject combining the pair with an explicit `--context` or `--coolify-config`. Report that the invocation pair overrides any committed context, and validate the binding on that instance. Do not mix a token from one source with a URL from another. This is a proposed Coolship extension, not an existing Coolify CLI environment-variable contract.

Resolve credentials once per invocation, retain them only inside authentication/client objects, and exclude them from context serialization and diagnostics. `link` does not change the Coolify CLI default or create a second credential database. `login` is the one writer: it verifies the URL and token against `GET /version` and `GET /teams/current` before saving, round-trips fields it does not know (Coolify CLI's `lastUpdateCheckTime`, anything on other instances), makes the first instance the default, and writes atomically with the same permissions Coolify CLI uses (directory 0750, file 0600). The token is read without echo or from stdin, never from a flag.

### Local state

The MVP can resolve fresh on each command. Add `.coolship/state.json` only when avoiding repeated reads has demonstrated value. Reserve it for schema-versioned, disposable UUID mappings; ignore `.coolship/` in Git when introducing it.

A cache key must include the normalized instance URL, context identity, effective selectors and pins, selected target key, configuration fingerprint, and resolution schema version. Recheck visibility and parent-child membership using the current token before trusting cached IDs. Token/team changes, environment overrides, missing resources, and changed selectors must never reuse an unverified binding.

Use atomic file replacement. Cache corruption is a miss, not permission to choose a different application. A missing cache must not break a valid committed configuration. Cache entries never contain tokens, secret values, full API responses, or authoritative defaults. Start without a cache rather than letting cached state define project identity.

## 6. Resource resolution and linking

`resolver` implements one hierarchy: selected instance/token scope, then project, then environment within that project, then application within that environment. Match names exactly and case-sensitively. Zero matches produce a missing-resource error; multiple matches produce an ambiguity error with non-secret identifying choices. Never select the first match.

Use the project environment list to resolve a UUID before retrieving environment details. Verify the returned environment identity and application membership. Explicit pins bypass name matching, but not scope or membership checks. Follow supported pagination when listing resources; incomplete candidate lists must not become successful unique matches. Convert read failures into typed errors instead of pretending the candidate list is empty.

### `link` flow

```mermaid
sequenceDiagram
    participant Command as cmd/link + ui
    participant Service as service.LinkSession
    participant Project as project
    participant Auth as auth
    participant Resolver as resolver
    participant API as coolify
    Command->>Service: BeginLink(paths, overrides)
    Service->>Project: Discover local project, allow missing config
    Service->>Auth: List context identities without tokens
    Service-->>Command: Context choices
    Command->>Service: Select context
    Service->>Auth: Resolve credentials
    Note over Service,API: Injected factory creates one backend
    Service->>Resolver: List scoped candidates through backend
    Resolver->>API: Read projects / environments / applications
    Service-->>Command: Choices at each selection step
    Command->>Service: Supply selected identities
    Service->>Resolver: Validate complete binding
    Service-->>Command: Binding plan and destination path
    Command->>Service: Apply reviewed binding plan
    Service->>Project: Write configuration atomically
    Service-->>Command: Linked result
```

The diagram condenses repeated project/environment/application selection into one stage. The service owns selection state and scoped candidate retrieval. `cmd` and `ui` display those choices and return selections; they do not reproduce lookup logic. Changing a parent selection clears its descendants.

The same service accepts complete selectors from noninteractive flags. Missing required selections in CI return an actionable error; no prompt fallback chooses a remote target. Linking an existing application makes no remote mutations. Review existing configuration before replacing a different binding; define an explicit replacement option for noninteractive use. A cancelled prompt writes nothing.

The binding plan includes the original file fingerprint. Before writing, detect concurrent edits and fail instead of overwriting them. Preserve unrelated configuration and comments when updating a binding. If lossless editing is not available initially, restrict writes to new files and explicitly reviewed replacements rather than silently regenerating existing TOML. Write through a temporary file and rename; a later cache write failure must not invalidate an otherwise successful link.

`unlink` removes the selected local binding only. `init` is the one workflow that creates a remote resource, and only explicitly: it detects the repository and build pack, shows the complete plan, creates the application after confirmation (or `--yes`), and then hands the new application to the same binding step `link` uses — `writeBinding` composes the binding, verifies it through the resolver, and writes the file — so the two commands cannot drift. A directory whose target is already bound is refused before any request rather than re-pointed. Neither operation deletes a remote application.

## 7. MVP command behavior and API boundary

| Command | Service responsibility | Initial API operations under `/api/v1` |
| --- | --- | --- |
| `link` | Discover, select a scoped binding, validate, and persist it. | `GET /projects`, `GET /projects/{uuid}/environments`, `GET /projects/{uuid}/{environment_uuid}`, `GET /applications/{uuid}` as needed. |
| `init` | Read the repository's public remote and branch, detect the build pack, refuse a linked directory or a Compose project before any request, confirm the plan, create the application without deploying, then persist and verify the binding through `link`'s write step. `--deploy` reuses the deployment service on the verified session. Private repositories are out of scope: `POST /applications/private-github-app` requires a `github_app_uuid` and `/private-deploy-key` a `private_key_uuid`, both registered in Coolify beforehand. | `GET /servers`, the `link` reads, optionally `POST /projects`, then `POST /applications/public` with `instant_deploy: false`; with `--deploy`, `POST /deploy` and `GET /deployments/{uuid}`. |
| `status` | Resolve once and return observed application status and identity. Preserve unfamiliar server status strings. Add the newest history row when it can be read; an unreadable history is a warning. | Shared resolution, then `GET /applications/{uuid}` and `GET /deployments/applications/{uuid}?take=1`. |
| `deploy` | Resolve once, submit exactly the selected application, and observe the returned deployment UUID. | `POST /deploy` with the application UUID; `GET /deployments/{deployment_uuid}` while waiting. |
| `deployments` | Resolve once and return the newest rows of the application's history as typed summaries — status, commit, kind (deploy, restart, rollback, preview), source (api, webhook, manual), timestamps — never the build log the server includes for a sensitive-read token. | `GET /deployments/applications/{uuid}?take=N`. |
| `cancel` | Resolve once; take the named deployment, read it, and check its embedded owner is the linked application, or find the single queued or in-progress row of the history and refuse none or several; refuse any state the server cannot cancel before a request; confirm, then cancel and report the status the server set. | `GET /deployments/{uuid}` or `GET /deployments/applications/{uuid}?take=25`, then `POST /deployments/{uuid}/cancel`. |
| `stop` | Resolve once; leave an application that already reports `exited` alone with a warning, and stop every other status as Coolify's own UI does (a crash loop reports `restarting` or `degraded`); confirm, request the stop, then poll the status until it reports `exited` or the timeout passes, reporting the last status seen. | `POST /applications/{uuid}/stop`, then `GET /applications/{uuid}` while waiting. |
| `start`, `restart` | Resolve once; `restart` confirms first. Queue through the server action, which names a deployment, then observe it through the same observation `deploy` uses. A message-only answer is the server declining. | `POST /applications/{uuid}/start` with `force`, or `POST /applications/{uuid}/restart`; `GET /deployments/{deployment_uuid}` while waiting. |
| `logs` | Resolve once, fetch runtime logs, optionally follow snapshots. | `GET /applications/{uuid}/logs?lines=…&show_timestamps=true`. |
| `open` | Resolve the application URL, or the dashboard page `/project/{p}/environment/{e}/application/{a}` verified in `routes/web.php`; the command invokes an injected browser opener only when interactive. Only web URLs reach the opener. | Shared resolution. |
| `doctor` | Run each preparation step separately and report all of them; local checks continue past failures, remote checks stop at the first. Exit 1 on any failure. | `GET /version` (plain text), then shared resolution. |
| `config` | Report the effective local configuration and credential source after overrides. | None. |
| `unlink` | Delete the discovered configuration after confirmation, refusing if it changed since discovery. | None. |
| `preview` | Deploy the preview Coolify already holds for a pull request through the deployment service; the number comes from `--pr` or `GITHUB_REF`. A refusal (unknown pull request) is the server's receipt message, repeated with guidance. | `POST /deploy` with `uuid` and `pr`; `GET /deployments/{deployment_uuid}` while waiting. |
| `domain`, `domain set` | Show the application's domains; replace them after confirmation, then read the application back to report what the server kept. Compose applications are refused by the server, which the error explains. | Shared resolution; `PATCH /applications/{uuid}` with `domains`, `redirect`, `force_domain_override`. |
| `login`, `logout` | Verify and store an instance in the Coolify CLI configuration; remove one. | `GET /version`, `GET /teams/current`; no resolution. |
| `dev` | Run a local command in the application root with the target's runtime variables injected, through an injected process runner; the child's exit status becomes the exit code. | `GET /applications/{uuid}/envs`. |
| `env pull\|diff\|push` | Compare one scope of the application's variables with a local dotenv file; pull writes, push upserts in one bulk request and deletes by identity only with `--prune`. | `GET /applications/{uuid}/envs`, `PATCH /applications/{uuid}/envs/bulk`, `DELETE /applications/{uuid}/envs/{env_uuid}`. |

Register only implemented commands. Begin with shared `--cwd`, `--config`, `--context`, `--coolify-config`, `--environment`, and `--format` options where applicable. Use `logs -f`/`--follow`; avoid speculative aliases and flag proliferation.

### Deployment semantics

Coolship triggers Coolify's configured deployment source. The MVP does not upload the current worktree, push Git commits, or change the application's configured branch. A project-local command therefore does not imply that uncommitted local changes will be deployed. Explain that behavior in command help.

Default deployment behavior should wait, with `--no-wait` returning an explicitly queued result and deployment UUID. Validate that the response array contains the selected resource and a nonempty deployment UUID; an informational message alone is not a queued deployment.

Observe that exact UUID, not the latest deployment for the application, which could belong to another user. Interpret the server's [ApplicationDeploymentStatus](../coolify/app/Enums/ApplicationDeploymentStatus.php): `queued`, `in_progress`, `finished`, `failed`, and `cancelled-by-user`. Only a confirmed successful terminal state completes a waiting command successfully. Unknown states remain visible and bounded by a timeout. Report deployment completion separately from any separately observed application health; do not invent build or health-check phases absent from server data.

Build logs belong to deployment observation; `logs` means application runtime logs. The deployment record carries its build log as a JSON document; the service emits each visible entry once, ahead of the status change it led to, and continues without logs when the server withholds them or the document is unreadable. Interrupting observation stops local waiting and reports the known UUID; it does not automatically cancel the remote deployment.

Every workflow that queues a deployment — `deploy`, `preview`, `init --deploy`, `start`, `restart` — hands the UUID it was given to one observation function on the prepared session, so build-log streaming, terminal states, and timeouts cannot drift between commands. `start` and `restart` are the server's own actions: the first is a deployment of the configured source (Coolify has no container start), the second a restart-only deployment that the job turns into a full deployment for Dockerfile and Docker image build packs. `stop` is the one lifecycle change that is not a deployment; it is observed by polling the application status until it reports `exited` — the value `StopApplication` writes — since the server queues the job and answers before anything happens. Waiting for `exited` rather than for `running` to go away matters because the status in between can read `restarting` or `degraded`, and those are also the statuses a crash-looping application shows before any stop is asked for. `cancel` never guesses: without a UUID it needs exactly one queued or in-progress row; with one it reads the deployment and refuses to act on another application's.

### HTTP and log-follow policy

`coolify` owns escaped endpoint paths, encoded query parameters, response decoding, sanitized typed errors, and a configurable HTTP client. Normalize the instance URL once and append `/api/v1/` consistently. It must not read environment variables or context files itself.

Retry only suitable read requests with bounded, cancellation-aware backoff, respecting `Retry-After`. Do not blindly replay deployment POSTs after network errors or server failures: the inspected API has no established idempotency-key contract. Report uncertain submission and provide recovery guidance instead of risking duplicate deployments.

Log follow initially polls bounded snapshots with timestamps. The service compares ordered overlapping lines without treating equal message text as a unique identifier. Repeated identical messages must remain representable. Without a server cursor, no-overlap snapshots or container restarts can produce gaps or duplicates; report the reset/gap and emit the available snapshot. Do not promise lossless streaming. Surface persistent polling failures and stop promptly on cancellation or output failure.

## 8. Terminal output and error handling

Inject stdin, stdout, stderr, and terminal capabilities. Human results and requested log data go to stdout. Prompts, progress, diagnostics, and debug information go to stderr. Disable animation and color when the relevant stream is not a terminal; noninteractive execution must not block for input.

Use `--format json` for a stable finite result object. Long-running follow output uses newline-delimited JSON events, documented as a stream. JSON mode never mixes human progress into stdout. Render defined public results rather than serializing complete project contexts, credential structures, or raw API responses.

Lower layers return typed errors and wrap causes with `%w`; no lower layer calls `os.Exit`, prints an error, or swallows repeated failures. Cobra returns errors to the process boundary. UI presents one diagnostic and maps usage/configuration failures, operation failures, and interruption to consistent exit statuses. Preserve useful distinctions such as authentication failure, missing target, ambiguous target, cancelled deployment, and uncertain submission.

Debug output includes safe request metadata and status, not authorization headers or request/response bodies that may contain secrets. Variable diff output masks values by default. Raw application/build logs can themselves contain secrets: treat them as requested log output and do not also persist them in a debug cache.

## 9. Extension paths without premature implementation

### Monorepos

Implemented. `config.Config` holds either one `[project]` binding or named `[apps.<name>]` bindings, never both: a mixed file is rejected so it has one meaning. Target names match `[A-Za-z0-9][A-Za-z0-9_-]*`, and `default` is reserved for the single form.

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

`project.Select` takes the target name and the effective working directory recorded by discovery. An explicit name (`--target`, or a positional argument on `deploy`, `logs`, `status`, and `open`) wins; otherwise the target whose resolved root most specifically contains the directory is chosen. Equal roots or a directory outside every root are errors that list the choices; nothing is guessed. Roots are validated against the configuration root, including symlinks, exactly as in the single form. The resolver still receives one target, so no workflow changed.

Migration rules live in `project.Propose`, which both `link` and the writer use: adding a new named target keeps the others and needs no review; changing an existing target's binding needs review; converting between the single and named forms drops the other form's bindings and therefore needs review, which the prompt says explicitly. `link --target NAME` proposes the working directory relative to the configuration root as the root, so linking from `apps/web` records `apps/web`. An unchanged file is never rewritten, and a file that changed since discovery is a conflict.

### Environment variables

Implemented as `env pull`, `env diff`, and `env push` in `internal/service/env.go`, with `internal/envfile` owning the dotenv format. The commands live in `cmd/env.go` rather than a `cmd/env` subpackage: a subpackage would need exported plumbing for the shared options and error helpers, for no benefit at this size. The original design notes follow; the implementation keeps them.

Add service operations using the same preparation path. Keep a variable's identity and scope richer than `map[string]string`: preserve preview/regular scope, build-time/runtime flags, literal/shared attributes, and whether a value is available. Build-time and runtime are independent flags. Remote environment selection and preview-variable scope are different dimensions.

Pull must not write masked or omitted values as if they were real secrets: a withheld key is recorded as a comment, and a local value for it is left alone. Push must preserve unspecified metadata — on Coolify 4.3.18 an update that omits `is_literal`, `is_multiline`, or `is_shown_once` resets them, so updates restate the remote values — and never deletes remote keys without `--prune`. Diff returns a typed change plan with values masked for ordinary output, in JSON as well. Local `.env` handling preserves comments and ordering, creates files with mode 0600, and replaces them atomically. Shared references compare and sync by their template, never by resolved value; `dev` is where resolved values belong. None of this required restructuring discovery or authentication.

### Development and previews

`dev` is implemented as designed: the service resolves the target, fetches the selected scope's runtime variables — injecting resolved values, where `env pull` keeps references — and hands a `ProcessSpec` (directory, argument vector or shell line, injected pairs) to a runner injected through `Dependencies`. `internal/process` owns execution: the platform shell for a configured `dev` line, the inherited environment beneath the injected pairs, an interrupt forwarded on cancellation with a kill after a grace period, and the exit status, which `service.ExitError` carries to the process exit code without a second diagnostic. No local runtime, proxy, Docker manager, or process handle lives in `project.Context`.

`preview` is implemented against the verified 4.3.18 contract. `POST /deploy` accepts `pr`, but only for a pull request Coolify already holds as an `ApplicationPreview`; otherwise it answers HTTP 200 with a receipt carrying a message and no deployment UUID. The API exposes `PATCH` and `DELETE` on `/applications/{uuid}/previews/{pull_request_id}` and nothing that creates or lists previews; creation happens through the UI or the `pull_request` webhook at `/webhooks/source/github/events/manual`, gated by `is_preview_deployments_enabled`. Coolship therefore deploys and observes an existing preview through the same deployment service, taking the number from `--pr` or from `GITHUB_REF`, and surfaces the server's refusal with guidance. Verified live: a webhook-created preview for a public repository deployed in 12 seconds. Preview URLs follow the application's `preview_url_template` but are not readable through the API, so `open` does not offer them.

### Possible upstream integration

**Decision: no shared package yet; keep the seam ready.** With every command from the brief implemented, the code that would move is clear, and so is what blocks moving it.

What transfers as-is: `config` (the two-form TOML schema and its validation), `project` (bounded discovery, target selection, reviewed writes), `resolver` (scoped, exact-match resolution with pins), `envfile`, `process`, and the workflows in `service`, whose only dependencies are those packages, `models`, `auth`, and `context.Context`. `cmd` transfers as constructors that take an `Application` interface and injected process concerns, which is how Coolify CLI already registers commands.

What Coolify CLI would need to supply behind the seams: an implementation of `service.Backend` over its own `internal/api` client (Coolship's `coolify` adapter is deliberately small and would be dropped, not upstreamed), an implementation of the three `auth` functions over its own `internal/config` (same file format, so this is thin), and its own `output` formatter behind Coolship's `ui` result types.

What blocks a shared package today, in order of weight:

1. **`internal/` on both sides.** A shared module must be a public Go module with a supported API. Neither project has agreed to own one, and the interfaces here have existed for one milestone; extracting them now would freeze names that live validation only just settled.
2. **Model differences.** Coolify CLI's models carry display tags and many more fields; Coolship's are the minimum each workflow needs (`DeployRequest`, `EnvironmentVariable` with nullable values). A shared package would have to choose one vocabulary.
3. **Go floor.** Both declare `go 1.26`, which is kept deliberately (see `AGENTS.md`), so this is not a blocker — it is listed because it must stay true.

The realistic path is therefore a pull request against Coolify CLI that adds `link`, `deploy`, `logs`, and `status` as project-local commands using Coolship's `config`, `project`, and `resolver` code verbatim under its `internal/`, with adapters for its client and credentials. Extract a shared module only if both projects then want to stop copying. Until that conversation happens, Coolship keeps `internal/` and this section records the plan. The reference repositories were not modified.

## 10. Implementation sequence and validation gates

All five gates are complete. [ROADMAP.md](ROADMAP.md) tracks the same sequence as milestones.

1. **Foundation — done.** The module, explicit command constructors, configuration codec, discovery, and the read-only credentials adapter. Nested-directory behavior, Git worktrees, explicit paths, malformed configuration, context precedence, and CI credentials are proven with temporary directories and synthetic credentials.
2. **Binding — done.** The minimal HTTP adapter, resolver, and link service. `httptest.Server` and fakes prove hierarchy checks, duplicate names, explicit pins, cross-environment rejection, prompt cancellation, and conflict-aware writes. The same selection rules apply interactively and noninteractively.
3. **Vertical workflows — done.** Status, deployment, and runtime logs go through the shared preparation path. Tests prove each command constructs one authenticated backend and uses the same resolver, and cover queued versus finished results, the exact deployment UUID, no POST replay, snapshot resets, cancellation, and human/JSON stream separation with injected writers.
4. **Live compatibility check — done.** Verified against Coolify 4.3.18 (below), with sanitized fixtures for hierarchy lookup, deployment responses and statuses, runtime logs, and permission-limited responses in `internal/coolify/testdata/`. The container-selection mismatch was confirmed on the server, so no such flag is exposed. The supported baseline comes from that evidence, not from the fork's apparent API surface.
5. **Extension proof — done.** `env pull`, `env diff`, and `env push` added endpoint and workflow code without duplicating discovery, auth loading, context selection, client construction, or application lookup; `preview`, `dev`, `domain`, and `init` followed the same seam. Administration commands remain out of scope.

### Supported server baseline

**Verified: Coolify 4.3.18**, reached through Cloudflare, with a token holding read, write, deploy, and sensitive-read abilities. The reference checkout is 4.3.19 (`424dbd3`); a transient fetch of upstream tag `v4.3.18` showed no change between the two in `routes/api.php`, `DeployController`, `ProjectController`, the sensitive-data middleware, or the deployment-status enum. The only `ApplicationsController` change is inside application creation, which `init` calls; that path was verified live on 4.3.18 itself (below). The source observations in section 1 therefore describe the verified server exactly for every endpoint Coolship uses.

Validation ran `link`, `status`, `deploy` (observed to `finished` in 29 s), `logs`, and `logs --follow` against a Dockerfile application created for that purpose ([example-coolify-project](https://github.com/joaomnuno/example-coolify-project)), plus read-only `link`, `status`, and `logs` against a pre-existing application. The lifecycle commands were later run on the same application in sequence: `deployments` (14 rows, two of them pull request previews), `stop --yes` (`running:healthy` → `exited:unhealthy` in 3 s), `status`, `start` (observed to `finished` in 25 s), `status` (`running:healthy`), `deploy --no-wait` followed at once by `cancel --yes` (`cancelled-by-user` 2 s after submission, application untouched), `cancel` again on the same UUID and on an unknown one (both refused before a request), and `restart --yes` (observed to `finished` in 25 s), ending `running:healthy`. Sanitized fixtures preserving the observed response shapes live in `internal/coolify/testdata/`; they contain no identifiers, hostnames, or values from the validating instance.

Observed behavior that shapes the client, none of which was visible from source alone:

- **Runtime log snapshots omit the newline after the final line.** Overlap detection therefore compares lines without terminators, and every emitted chunk ends with a newline. The original terminator-sensitive comparison reset on every poll.
- **`GET /version` returns plain text** (`4.3.18`) with an HTML content type, not JSON.
- **Deployment logs are present** when the token can read sensitive data: `logs` is a JSON-encoded string holding an array of `{batch, command, hidden, output, timestamp, type}`; roughly half the entries are `hidden`. Without that ability the field is absent, which the client reports as unavailable rather than empty.
- **A deployment response identifies its application by integer `application_id` and `application_name`**, not by UUID. Coolship verifies the returned `deployment_uuid` against the one it submitted.
- **Creating a regular environment variable also creates a preview-scope twin** through the model's `created` hook. Preview scope is a separate dimension that every variable workflow selects explicitly.
- **`is_shown_once` withholds `value` and `real_value`** from the regular row, but the auto-created preview twin is returned with the value in clear. Coolship never reads around a withheld value; the twin is a server defect to report upstream.
- **The variable API accepts `is_buildtime` and `is_runtime`**; `is_build_time` is rejected with 422. Bulk creation is `PATCH /applications/{uuid}/envs/bulk` with `{"data": [...]}`; deletion is by variable UUID.
- **`POST /deploy` accepts `pr`** (pull request id) alongside `uuid` and `force`, and answers HTTP 200 with a message-only receipt when the pull request has no preview record. No endpoint creates previews; section 9 defines `preview` against that.
- **`POST /applications/public` answers 201 with `{uuid, domains}`** and, without `domains` in the request, assigns the generated `https://<uuid>.<wildcard>` domain at once. A GitHub URL is stored as `owner/repo` with the built-in public GitHub source (`source_id` 0); other hosts keep the full URL. A never-deployed application reports `exited:unhealthy`. A refusal is `422 {"message": "Validation failed.", "errors": {field: [...]}}`, which the client surfaces for 4xx mutations. `DELETE /applications/{uuid}` queues a `DeleteResourceJob` and answers 200; the application is gone from reads within seconds. Verified by creating `coolship-init-test` from the example repository and deleting it again.
- **A Cloudflare bot rule in front of the validating instance rejects some default user agents.** Coolship sends `coolship/<version>`; Coolify CLI sends Go's default. Both are accepted; a generic scripting-language default was not.
- **`GET /applications/{uuid}/logs` answers `400 {"message": "Application is not running."}`** when the application has no running container. The adapter reads that one refusal body, on that endpoint only, and returns a typed not-running error; the workflow reports it with the application's observed status. Any other 400 body, there or elsewhere, stays private as before.
- **`POST /deploy` for a commit that is already queued or in progress.** Three `--no-wait` submissions within two seconds were each answered `queued` with their own `deployment_uuid`; the first ran and finished, and the other two later answered `404 {"message": "Deployment not found."}` — the server had dropped them. A submission five seconds after one that was in progress was answered 200 with `Deployment already queued for this commit.` and, as `DeployController::deploy_resource` shows, the `deployment_uuid` it had generated before asking `queue_application_deployment`, which never queued it. Coolship therefore treats that message as a refusal whatever the receipt's UUID and suggests `--force`, and reports a 404 while observing a deployment as dropped rather than as "may still be running". The queue-full refusal — 429 `Deployment queue is full. Please wait for existing deployments to complete.` with `Retry-After: 60`, once the server holds `deployment_queue_limit` (default 25) queued deployments — is mapped but was not triggered.
- **An `http://` URL is answered with a 301** to https by the proxy in front of the instance. The client never follows redirects, so the status is reported with the hint to use the https URL.
- **A host name the certificate does not cover answers a TLS alert** (`remote error: tls: handshake failure`) rather than a certificate error; it is reported as a refused handshake, distinct from an unverifiable certificate.

Lifecycle and history, verified on 4.3.18 against the same application on 2026-09-10 (`DeployController::get_application_deployments`, `cancel_deployment`, `deployments`; `ApplicationsController::action_stop`, `action_deploy`, `action_restart`; `Application::deployments`; `queue_application_deployment`; `StopApplication`; `ApplicationDeploymentJob` in the reference checkout):

- **`GET /deployments/applications/{uuid}` answers `{"count": N, "deployments": [...]}`**, newest first by `created_at` then `id`, with `skip` (default 0) and `take` (default 10, minimum 1). Each row is the queue record: `deployment_uuid`, `status`, `commit`, `commit_message`, `pull_request_id`, `force_rebuild`, `restart_only`, `rollback`, `is_webhook`, `is_api`, `server_name`, `created_at`, `updated_at`, `finished_at` (null while running), plus internals such as `horizon_job_id`. **`logs` is included in every row** when the token may read sensitive data (the model hides it otherwise), so a 14-row page carried about 200 KB of build logs; Coolship decodes into a record without that field. `commit` is the placeholder `HEAD` until the job resolves the sha, and stays `HEAD` on a deployment cancelled before that. There is no `started_at`; `created_at` is when it was queued.
- **`POST /applications/{uuid}/stop` answers `{"message": "Application stopping request queued."}`** and dispatches `StopApplication` on the queue; `docker_cleanup` (query, default true, the same default as the UI's stop dialog) is left to the server. The status went from `running:healthy` to `exited:unhealthy` by the first poll, 3 s after the request; the job sets `status` to `exited` and the monitor refines it. The status is `<state>:<health>`, where the state is the container's (`running`, `exited`, `restarting`, `created`, `paused`), `degraded` for a recent crash loop, or `starting` for a swarm replica (`GetContainersStatus`); `action_stop` has no status precondition, and the UI offers Stop for every status not prefixed `exited` (`heading.blade.php`). Coolship applies the same rule, so a crash loop shown as `restarting` or `degraded` can be stopped; that path was verified against the source, not live, since the example application does not crash.
- **`POST /applications/{uuid}/start` is `action_deploy`**: it queues an ordinary deployment (`force` and `instant_deploy` accepted) and answers a flat `{"message": "Deployment request queued.", "deployment_uuid": "..."}` — not the `deployments` array `POST /deploy` returns. When `queue_application_deployment` skips (a deployment for the same commit already queued or running), the answer is `{"message": "Deployment already queued for this commit."}` without a UUID, although the helper knows the existing one. A full queue is not distinguished by the action endpoints: they answer with a UUID that was never created (source observation; not reproduced live). Observed live: queued → in_progress → finished in 25 s, build step skipped because the commit's image existed, rolling update ran. Right after a rolling update the application status can still read `running:unhealthy` (seen 1 s after a restart's deployment finished; `running:healthy` a minute later), since the status is refreshed by the server's monitor rather than by the deployment job.
- **`POST /applications/{uuid}/restart` answers `{"message": "Restart request queued.", "deployment_uuid": "..."}`** and records `restart_only: true` on the row, which `deployments` shows as `restart`. `ApplicationDeploymentJob` clears `restart_only` for the `dockerfile` and `dockerimage` build packs, so the example application was deployed in full (23 s, image reused, rolling update); for other build packs `just_restart` skips the build when the image exists.
- **`POST /deployments/{uuid}/cancel` answers `{"message": "Deployment cancelled successfully.", "deployment_uuid": "...", "status": "cancelled-by-user"}`** for a `queued` or `in_progress` row, marks it in the database first, then kills the build container and process if they exist, and advances the server's queue. Anything else is `400 {"message": "Deployment cannot be cancelled. Current status: finished"}`; another team's deployment is 403; an unknown UUID 404. Cancelling a deployment submitted with `--no-wait` one to two seconds earlier appended "Deployment cancelled by user via API." and "Deployment container not yet started. Will be cancelled when job checks status." to its log; when the job had already started it logged "Deployment cancelled by user, stopping execution." and set `finished_at` in its cleanup, but `finished_at` is written only there (`ApplicationDeploymentJob`'s `finally`), so a deployment cancelled before the job reached it keeps `finished_at` null for good — seen on the second live cancel — and Coolship shows no duration for such a row. The running container was untouched both times: the application stayed `running:healthy`.
- **`GET /deployments/{uuid}` embeds the owning `application` document**, whose `uuid` Coolship checks before cancelling a named deployment. `GET /deployments` lists the team's `queued` and `in_progress` rows across servers (empty when idle), identifying applications by integer `application_id` only, so Coolship reads the application's own history instead.

Limits that remain, independent of the version:

- **Deployment states** are interpreted as `queued`, `in_progress`, `finished`, `failed`, and `cancelled-by-user`. An unrecognized state is reported verbatim and waits until the timeout rather than being guessed as terminal.
- **Log follow** has no server cursor. Rotation or a gap larger than the requested line count can still produce gaps or duplicates, which Coolship reports rather than hiding.
- **Container selection** is not exposed: the server's log handler ignores the `service_name` parameter Coolify CLI sends and returns the first container.
- **Pagination** is not assumed. A `Link` header advertising a next page is refused rather than treated as a complete candidate list.
- **Deployment submission is never replayed.** The API defines no idempotency key, so an uncertain `POST /deploy` result is surfaced with the information needed to recover manually.

Run focused behavioral tests while developing, then `./scripts/go test ./...`, `./scripts/go test -race ./...`, and `./scripts/go vet ./...` for a milestone.

The live server baseline is recorded above. The package boundaries, configuration precedence, binding semantics, and MVP command behavior are settled decisions.
