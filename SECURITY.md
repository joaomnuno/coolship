# Security policy

## Supported versions

Coolship is before 1.0, so only the latest release gets security fixes. Fixes land on `main` and ship in the next release; older versions are not patched.

| Version | Supported |
| ------- | --------- |
| 0.4.x   | Yes       |
| < 0.4   | No        |

## Reporting a vulnerability

Please do not open a public issue, discussion, or pull request for a security problem.

Report it privately through GitHub instead: open the repository's [Security tab](https://github.com/joaomnuno/coolship/security) and choose "Report a vulnerability". Only the maintainer can see the report.

Include the Coolship version (`coolship --version`), your Coolify version, and the steps to reproduce the problem. Leave out real tokens and secret values; a made-up token shows the problem just as well.

Coolship is maintained by one person, so there is no fixed response time, but every report is read and answered. Once a fix is ready, it is released, the changelog entry describes it, and you are credited in the advisory unless you would rather not be.

## What counts

Examples of problems to report here:

- A Coolify token, or a secret value read from the server, printed to the terminal, written to a log, or saved into a project file.
- `scripts/install.sh` installing a binary that does not match the published checksums.
- A command that reads or writes a Coolify resource other than the one the project is linked to.

A vulnerability in Coolify itself is not a Coolship problem, even when Coolship is how you noticed it. Report it to Coolify through [its security policy](https://github.com/coollabsio/coolify/security/policy).
