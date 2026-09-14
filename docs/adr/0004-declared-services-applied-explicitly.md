---
status: proposed
---

# Declared services, applied explicitly

A project's application usually needs a database or a cache. Coolify can create both through its API: `POST /api/v1/databases/{type}` creates a standalone database, and `POST /api/v1/services` creates a one-click service. Neither create endpoint is idempotent, and neither connects the new resource to an application. We propose declaring these resources in `coolship.toml` as `[services.<name>]` tables with a type and a version. An application would refer to a service's connection URL with `{{services.<name>.url}}`, and Coolship would resolve that reference into a Coolify variable on the application. The resolved value would never be written to the file.

The service name would be the stable key. A service would resolve by exact name inside the linked project and environment, and it could be pinned by UUID, as applications are. Removing or renaming a service in the file would never delete anything on the server. Only an explicit `services prune` would delete a service, after naming it and asking.

The open question is when services are created. The recommendation is explicit `coolship services plan` and `coolship services apply` commands. `deploy` would check that the declared services exist and are running. If they do not, an interactive run would offer to create them after showing the plan, and a non-interactive run would fail unless `--yes` is given. Two alternatives were considered. Creating services during `init` handles the first setup but not a service added to the file later. Creating them during every `deploy` would make a routine deployment slow and let it create resources without showing a plan, which `ARCHITECTURE.md` rules out.

## Consequences

These would apply once the proposal is implemented.

- Coolship would create resources other than applications, but only inside the linked project and environment. Server administration and proxy orchestration would remain out of scope.
- Reading a connection URL would need a token with sensitive read. Without it, Coolship would report the missing ability and not write an empty URL.
- Standalone databases share a network with applications on the same server only, so Coolship would refuse a reference that crosses servers.
- The same tables would be the input for running the stack locally in v0.6.
- Backups and restores would not be included.
