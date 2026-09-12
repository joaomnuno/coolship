# Contributing to Coolship

Coolship is a small project, and a good bug report is as useful to it as a pull request. This guide covers how to report a problem, how to set up the code, and what a change needs before it can be merged.

## Before you start

Check the open [issues](https://github.com/joaomnuno/coolship/issues) and [pull requests](https://github.com/joaomnuno/coolship/pulls) first. If an issue already describes what you want to fix, leave a comment saying you are working on it, so two people do not solve the same problem in parallel. For anything larger than a small fix, open an issue before writing code and agree on the approach there.

Coolship is a project-local CLI: it answers "I'm inside this project, how do I deploy it?" and deliberately stops short of administering a Coolify instance. Changes that add server, team, or key management belong in [`coolify-cli`](https://github.com/coollabsio/coolify-cli) instead. [README.md](README.md) has the longer version under "What Coolship is not", and [ROADMAP.md](ROADMAP.md) lists what is planned.

## Reporting a bug

A useful report says what you ran, what you expected, and what happened instead. Include the output of `coolship --version`, your Coolify version, your operating system, and the full command with its output. `coolship doctor` often explains the problem on its own, so its output helps too.

Never paste a token. Coolship does not print tokens or secret values, but check anything you copy from a shell history, a CI log, or a `.env` file before you post it.

## Reporting a security problem

Do not open a public issue for a vulnerability. Use the "Report a vulnerability" button under the repository's Security tab, which sends the report to the maintainer privately. A problem in Coolify itself should go to Coolify through [its own security policy](https://github.com/coollabsio/coolify/security/policy).

## Setting up

Coolship needs Go 1.26 or newer. The `go` directive stays at 1.26 to match `coolify-cli`, so that code can move upstream later; a newer toolchain builds it unchanged.

```bash
git clone https://github.com/joaomnuno/coolship.git
cd coolship
scripts/build
bin/coolship --version
```

Use `./scripts/go` in place of `go` for every Go command. It keeps the build cache, the module cache, and temporary test files inside `.cache/` in the repository rather than in your home directory.

The tests never need a Coolify instance or real credentials. They use temporary directories and `httptest` servers, and new tests should do the same.

## Making a change

[AGENTS.md](AGENTS.md) holds the working rules and [ARCHITECTURE.md](ARCHITECTURE.md) explains the design; read both before a change that touches more than one package. The rules that come up most often in review are these. Cobra commands live in `cmd/` and call `internal/service`, which does the work. Discovery and local writes stay in `internal/project`, the TOML schema in `internal/config`, credentials in `internal/auth`, HTTP in `internal/coolify`, and presentation and prompts in `internal/ui`. Lower layers return typed data and errors, and an error is printed once, at the executable boundary. Pass `context.Context` explicitly and keep every HTTP request cancellable. A deployment `POST` is never replayed automatically after an uncertain response.

Write a test that fails without your change and passes with it wherever the change can be tested in Go. Keep one topic per pull request; a fix and an unrelated refactor are easier to review as two.

## Checks to run before pushing

These are the same checks CI runs:

```bash
./scripts/go test ./...
./scripts/go test -race ./...
./scripts/go vet ./...
gofmt -l cmd internal main.go
```

`gofmt -l` should print nothing. If you changed anything under `scripts/`, also run:

```bash
shellcheck --exclude=SC1007 scripts/*
sh scripts/install_test.sh
```

`scripts/install.sh` is POSIX `sh`, not Bash, and the installer tests run it under `dash`. The archive and checksum names in `.goreleaser.yaml` are a contract with `scripts/install.sh`, so change them together or not at all.

## Changelog

Every user-visible change adds an entry under `## [Unreleased]` in [CHANGELOG.md](CHANGELOG.md), in the same pull request. Put it in the right subsection (Added, Changed, Fixed, or Removed) and write it in the past tense from the user's point of view: what they saw before and what they see now. The existing entries show the level of detail expected.

CI fails a pull request that changes Go code under `cmd/`, `internal/`, or `main.go` without touching `CHANGELOG.md`. If your change is an internal refactor that no user can observe, say so in the pull request and a maintainer will apply the `skip-changelog` label.

## Documentation

When behavior changes, update what describes it in the same pull request: the command's section in [README.md](README.md) and its page under `docs/content/docs/`. Do not document a planned command or flag as if it already exists.

The documentation site is a Fumadocs application in `docs/`. To work on it:

```bash
cd docs
npm ci
npm run check      # route types and TypeScript
npm run build
npm start          # serves the built site on port 3000
npm run smoke      # in another terminal, checks the main routes
```

If you rename a heading or a callout that `docs/scripts/smoke.sh` looks for, update the script in the same change.

## End-to-end tests

`scripts/e2e` runs the CLI against a live Coolify instance. It writes to exactly one application, `coolship-example` in the project `coolship-example`, and the application must already exist there. The maintainer runs it through the e2e workflow, weekly and on every release, so you do not need an instance of your own to contribute.

If you do have one and want to run it, export `COOLSHIP_URL` and a `COOLSHIP_TOKEN` with read, write, and deploy abilities, create that project and application, and run `scripts/e2e`. It also needs GNU `timeout` and `python3`.

## Commits and pull requests

Write commit subjects in the imperative and in sentence case, saying what the change does, for example "Keep credentials out of remote errors". The body, when there is one, explains why.

A pull request description states the problem, the fix, and anything a reviewer should look at closely. End it with `Closes #N` for each issue it resolves. Pull requests are merged by rebase, so keep your branch free of merge commits; the maintainer may rebase it onto `main` before merging.

Four checks must pass before a merge: Go, Changelog entry, Shell scripts, and Installer. A change under `docs/` also runs two container image checks. They are not required by branch protection, but a pull request with either of them failing will not be merged.

## License

Coolship is released under the [MIT License](LICENSE). By contributing, you agree that your contribution is released under the same license.
