# Changelog

All notable changes to Coolship are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/). Before 1.0.0, minor versions may
change command behavior; the changelog says when they do.

## [Unreleased]

### Added

- `init`: create a Coolify application for the current repository from its public Git remote, then link it exactly as `link` would. The remote and branch come from Git (SSH forms are normalized to https), the build pack from the application root (`Dockerfile` or Nixpacks; Docker Compose is refused), and the plan — repository, branch, build pack, port, project, environment, server, name — is confirmed before anything is created, or requires `--yes`. `--project` with `--create-project` creates a missing project; `--server` names the server, otherwise the only usable one is used; `--deploy` submits and observes the first deployment. A directory that is already linked is refused. Private repositories are not supported: the API needs a GitHub App or deploy key registered in Coolify.
- Refusals of creation and other mutations now carry the server's explanation (its `message` and field errors) in the error, so a repository Coolify cannot reach or a rejected value is reported as such rather than as a bare status code. Reads and server faults still report the status alone.

## [0.2.0] - 2026-09-10

Every command is now installable from a release, and the tool logs you in itself.

### Added

- `login` and `logout`: save a Coolify instance and API token after verifying them against the server, in the same file coolify-cli uses, so both tools share one login. The token is read without echo, or from stdin with `--token-stdin`; it is never a flag.
- A `LICENSE` file (MIT).
- Tagged releases: pushing a `vX.Y.Z` tag publishes a GitHub Release with archives for Linux, macOS, and Windows on amd64 and arm64, a `checksums.txt`, and notes taken from this changelog. Tags with a pre-release suffix such as `v0.2.0-rc.1` are published as pre-releases, and `latest` always points at the newest full release.
- A rolling `nightly` pre-release built from `main` on demand, replaced on every run and never marked `latest`.
- Continuous integration on every push and pull request: `gofmt`, `go vet`, tests with and without the race detector, builds, `shellcheck` on the scripts, and a check that pull requests changing Go code also update this changelog unless labeled `skip-changelog`.
- `scripts/install.sh`: a curl-able installer for Linux and macOS that downloads a release archive from GitHub Releases, verifies its SHA-256 against `checksums.txt`, installs into `$HOME/.local/bin` (or `COOLSHIP_INSTALL_DIR` / `--dir`), and prints a PATH hint for bash, zsh, or fish. `COOLSHIP_VERSION` / `--version` selects a release, including pre-releases; `--dry-run` shows what would happen. It never runs `sudo`.
- `scripts/e2e` and the `e2e` GitHub Actions workflow: every command is exercised against a live Coolify instance, confined to one example application, weekly and on each published release. The workflow is skipped when the `COOLSHIP_URL` and `COOLSHIP_TOKEN` secrets are absent.
- Styled human output on terminals: `doctor` markers, deployment statuses, `Warning:` and `Error:` prefixes, prompt questions, `env diff` markers, and key/value labels are colored with plain ANSI sequences. Styling is decided per stream, so a piped stdout stays plain while prompts on a terminal stderr are styled; `NO_COLOR`, `TERM=dumb`, `CI`, and the new `--no-color` flag turn it off. `--format json` and streamed logs are never styled.
- A one-page site in `site/`, published to GitHub Pages from `main` and buildable as an nginx image for deploying on Coolify.

## [0.1.0] - 2026-09-09

First release. Every command from the original brief is implemented and was
verified end to end against a live Coolify **4.3.18** instance, using a public
example repository and an isolated project on that instance.

### Added

- `link`: bind a repository to an existing Coolify application, interactively or with explicit selectors, writing a credential-free `coolship.toml`. Names are matched exactly within their parent; UUID pins are written when a name is ambiguous.
- `status`, `deploy`, `logs`: inspect, deploy, and read runtime logs of the linked application. `deploy` observes exactly the deployment it submitted and streams the server's build log; `logs --follow` polls snapshots and reports gaps rather than hiding them.
- `open`: print or open the application's URL, or its Coolify page with `--dashboard`.
- `unlink`, `config`, `doctor`: remove the binding, show the effective configuration, and check every preparation step with a failing exit status.
- `env pull`, `env diff`, `env push`: synchronize a local dotenv file with one scope of the application's variables. Preview scope is explicit, withheld values are never invented, shared references are never flattened, and pushed updates preserve flags the server would otherwise reset.
- `preview`: deploy the preview Coolify holds for a pull request, from `--pr` or `GITHUB_REF`.
- `dev`: run a local command with the application's runtime variables injected, exit status and signals propagated.
- `domain`, `domain set`: show and replace the application's domains, reading the application back to confirm.
- Named monorepo targets: `[apps.<name>]` tables selected by directory or by name, with add-without-review and change-with-review migration rules.
- Credentials from the Coolify CLI configuration, or from `COOLSHIP_URL` and `COOLSHIP_TOKEN` for CI. A `User-Agent` identifying Coolship.
- `--format json` on every command, with values masked by default where they may be secrets.

### Compatibility

- Verified against Coolify 4.3.18; the 4.3.19 source has no changes to any endpoint used. Other versions are untested.
- Requires Go 1.26 to build, matching `coolify-cli`.

[Unreleased]: https://github.com/joaomnuno/coolship/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/joaomnuno/coolship/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/joaomnuno/coolship/releases/tag/v0.1.0
