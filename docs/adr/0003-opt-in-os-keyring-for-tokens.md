---
status: proposed
---

# Opt-in OS keyring for tokens

Coolship reads and writes Coolify CLI's credentials file, so one login serves both tools. The file keeps each token in plaintext at mode 0600, and backups, dotfile sync, and accidental commits or copies take the token along with the file. ADR 0002 left a keyring as a later opt-in. We propose adding it in v0.5 as an opt-in, with the plain file kept as the default.

A keyring removes the token from disk, so it would protect against plaintext at rest, backups, dotfile sync, and accidental commits or copies. It would not protect against other processes running as the same user. Secret Service, Credential Manager, and an unlocked Keychain hand the secret to any process in the user's session, apart from the access prompts Keychain can show. Malware running as the same user stays out of scope.

Under this proposal, a context stored with `login --use-keyring` would keep its entry in the shared file, with a marker where the token would be, and its token would go into the OS keyring through `zalando/go-keyring`. That library is pure Go, so releases would still build with `CGO_ENABLED=0`. On macOS it calls `/usr/bin/security`, on Linux it uses Secret Service over D-Bus, and on Windows it uses Credential Manager. `99designs/keyring` was rejected because it needs cgo on macOS and is no longer maintained. The flag name follows Wrangler, which is the one peer with the same opt-in. gh stores tokens in the keyring by default and uses `--insecure-storage` to opt out.

Tokens would resolve in one fixed order: the `COOLSHIP_URL` and `COOLSHIP_TOKEN` pair, then the keyring for a marked context, then the file. When the keyring cannot be reached, the command would fail with the reason. It would never fall back to the file quietly, which is what gh is criticised for in cli/cli#10108.

## Consequences

These would apply once the proposal is implemented.

- A context stored in the keyring would no longer work in Coolify CLI, which reads only the file and would send the marker as a token. That is why the keyring would be opt-in, and `login` would say so before it writes.
- `logout` would remove a context from both the file and the keyring. `doctor` would report where each token lives and whether the keyring can be read. A separate command would move a context into the keyring and back, restoring the original when any step fails.
- Headless Linux, WSL, and containers often have no Secret Service. Developers there would keep the plain file, and CI would keep the environment pair.
- Tests would use the library's `MockInit` fake and never touch a real keyring.
- If Coolify CLI adopts keyring storage later, the default could be reconsidered.
