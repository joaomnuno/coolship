---
status: proposed
---

# Opt-in OS keyring for tokens

Coolship reads and writes Coolify CLI's credentials file, so one login serves both tools. The file keeps each token in plaintext at mode 0600. Any process running as the same user can read it, and it ends up in backups and dotfile sync. ADR 0002 left a keyring as a later opt-in. We propose adding it in v0.5 as an opt-in, with the plain file kept as the default.

A context stored with `login --use-keyring` keeps its entry in the shared file, with a marker where the token would be, and its token goes into the OS keyring through `zalando/go-keyring`. That library is pure Go, so releases still build with `CGO_ENABLED=0`. On macOS it calls `/usr/bin/security`, on Linux it uses Secret Service over D-Bus, and on Windows it uses Credential Manager. `99designs/keyring` was rejected because it needs cgo on macOS and is no longer maintained. The flag name follows Wrangler, which is the one peer with the same opt-in. gh stores tokens in the keyring by default and uses `--insecure-storage` to opt out.

Tokens resolve in one fixed order: the `COOLSHIP_URL` and `COOLSHIP_TOKEN` pair, then the keyring for a marked context, then the file. When the keyring cannot be reached, the command fails with the reason. It never falls back to the file quietly, which is what gh is criticised for in cli/cli#10108.

## Consequences

- A context stored in the keyring no longer works in Coolify CLI, which reads only the file and would send the marker as a token. That is why the keyring is opt-in, and `login` says so before it writes.
- `logout` removes a context from both the file and the keyring. `doctor` reports where each token lives and whether the keyring can be read. A separate command moves a context into the keyring and back, restoring the original when any step fails.
- Headless Linux, WSL, and containers often have no Secret Service. Developers there keep the plain file, and CI keeps the environment pair.
- Tests use the library's `MockInit` fake and never touch a real keyring.
- If Coolify CLI adopts keyring storage later, the default can be reconsidered.
