# Changelog

All notable changes to Coolship are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/). Before 1.0.0, minor versions may
change command behavior; the changelog says when they do.

## [Unreleased]

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

[Unreleased]: https://github.com/joaomnuno/coolship/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/joaomnuno/coolship/releases/tag/v0.1.0
