---
status: accepted
---

# Follow peer deploy CLIs for conventions

For flag names, help layout, and where files live, Coolship copies what most deployment CLIs do rather than deciding case by case. A survey on 2026-09-11 of Wrangler, flyctl, Vercel, Netlify, Railway, Heroku, and gh set the following, and a later change to any of them needs a stronger reason than taste.

## What the survey found, and what Coolship does

- Every one of the seven mixes flat verbs with noun subcommands. Three group their help (Wrangler, flyctl, gh); the two Go ones use Cobra groups. Coolship keeps its verbs flat and groups the help like flyctl, with task-shaped names.
- No CLI has a short quiet flag. Verbose and debug are long-only where they exist (flyctl has both; Vercel has `--debug`), otherwise an environment variable (`WRANGLER_LOG`, `FLY_VERBOSE`, `GH_DEBUG`, `HEROKU_DEBUG`). `-v` means version in five of seven. Coolship uses `--verbose`, `--debug`, and `COOLSHIP_VERBOSITY`, and keeps `-v` as version.
- All seven keep per-user files under `~/.config/<tool>` or a dot directory; four keep preferences apart from the token. Coolship keeps preferences in its own file and the token where it is.
- Five of seven store the token as a plain file at mode 0600 with the directory at 0700; Heroku and gh default to the OS keyring, Wrangler and Vercel offer it as an opt-in. Coolship keeps the plain 0600 file, shared with Coolify CLI so one login serves both tools, and treats a keyring as a later opt-in. Only Heroku shares a credentials file with another tool (`.netrc`), so the sharing is Coolship's one deliberate deviation, kept because the brief asked for it.
- Vercel and Netlify, the two closest in shape, show a spinner per phase and hide the build log behind a flag or a link; Railway and Heroku stream it. Coolship follows Vercel and Netlify.
- Five of seven print help when run bare; flyctl prints a short welcome and Vercel deploys. Coolship prints help, and its menu is a separate command.
