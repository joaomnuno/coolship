/**
 * Everything the landing page says, in one place. Transcripts are real: the
 * `deploy` and `doctor` output is copied from README.md, and the `status`
 * and `logs` output was captured against the example application
 * (coolship-example, a Dockerfile app) with a local binary; only the working
 * directory and the context name were replaced with the README's
 * `/home/you/my-app` and `home`. The `link` output is what a terminal shows:
 * the checklist and one line, behind the left bar of internal/ui/block.go. JSON shapes come from the command reference.
 */
import type { Lang } from "./highlight";
import type { ReplayLine, ReplaySegment } from "./terminal-replay";

type Stage = { name: string; parent?: string; start?: number; end?: number };

/**
 * The checklist `coolship deploy` draws in a terminal, as rows the replay
 * redraws in place: the spinner (Bubbles' Dot) and a clock on whatever is
 * open, ✓ and the duration on what finished, and the stages not reached yet
 * dim, aligned on the same 30 columns as internal/ui/checklist.go. The
 * durations are those of the example application's deployment in README.md;
 * the snapshots between them are compressed so the replay stays short.
 */
function deployChecklist(): ReplayLine[] {
  const spinner = ["⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"];
  const clock = (s: number) =>
    `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
  const row = (glyph: string, label: string, width: number, time: string) =>
    `${glyph} ${label}${" ".repeat(Math.max(width - label.length, 1))}${time}`;
  const stages: Stage[] = [
    { name: "build", start: 1, end: 42 },
    { name: "rolling update", start: 42, end: 50 },
    { name: "container", parent: "rolling update", start: 45, end: 51 },
    { name: "cleanup", parent: "rolling update", start: 51, end: 51 },
  ];
  const snapshots = [0, 1, 7, 15, 24, 33, 41, 42, 45, 48, 50, 51, 52];
  const shown = new Map<string, string>();
  const lines: ReplayLine[] = [];

  snapshots.forEach((now, tick) => {
    const glyph = spinner[tick % spinner.length];
    const finished = now >= 52;
    const head = finished
      ? `✓ Deployed coolship-example to production in ${clock(now)}`
      : row(glyph, now === 0 ? "Deployment queued" : "Deployment in progress", 30, clock(now));
    const next: Array<[string, string, ReplayLine["kind"]]> = [["head", head, "out"]];
    for (const stage of stages) {
      const { name, start = Infinity, end = Infinity } = stage;
      // Container and cleanup run inside the rolling update and are drawn
      // under it once it has started.
      const nested = stage.parent !== undefined && now >= (stages.find((s) => s.name === stage.parent)?.start ?? Infinity);
      const indent = nested ? "    " : "  ";
      const width = nested ? 26 : 28;
      if (now < start || (now === start && start === end && !finished))
        next.push([name, `${indent}  ${name}`, "dim"]);
      else if (now < end) next.push([name, `${indent}${row(glyph, name, width, clock(now - start))}`, "out"]);
      else next.push([name, `${indent}${row("✓", name, width, clock(end - start))}`, "out"]);
    }
    let first = true;
    for (const [id, text, kind] of next) {
      if (shown.get(id) === text) continue;
      shown.set(id, text);
      lines.push({ id, text, kind, delay: first ? (tick === 0 ? 1100 : 380) : 0 });
      first = false;
    }
  });
  return lines;
}

export const replayScript: ReplaySegment[] = [
  {
    command:
      "coolship link --project coolship-example --environment production --application coolship-example",
    output: [
      {
        text: "┃ ✓ Choose the application        0:00  coolship-example / production / coolship-example",
        delay: 700,
      },
      { text: "┃ ✓ Write coolship.toml           0:00" },
      { text: "┃ coolship-example is linked." },
    ],
  },
  {
    command: "coolship deploy",
    output: [
      { text: "→ coolship-example", delay: 500 },
      { text: "→ production" },
      { text: "" },
      ...deployChecklist(),
      { text: "https://coolship.example.com", delay: 400 },
    ],
  },
  {
    command: "coolship logs --lines 5",
    output: [
      {
        text: '2026-09-10T14:15:01.017517155Z 127.0.0.1 - - [10/Sep/2026:14:15:01 +0000] "GET / HTTP/1.1" 200 182 "-" "Wget" "-"',
        delay: 600,
      },
      {
        text: '2026-09-10T14:15:11.049948936Z 127.0.0.1 - - [10/Sep/2026:14:15:11 +0000] "GET / HTTP/1.1" 200 182 "-" "Wget" "-"',
        delay: 40,
      },
      {
        text: '2026-09-10T14:15:21.089560996Z 127.0.0.1 - - [10/Sep/2026:14:15:21 +0000] "GET / HTTP/1.1" 200 182 "-" "Wget" "-"',
        delay: 40,
      },
      {
        text: '2026-09-10T14:15:31.130467890Z 127.0.0.1 - - [10/Sep/2026:14:15:31 +0000] "GET / HTTP/1.1" 200 182 "-" "Wget" "-"',
        delay: 40,
      },
      {
        text: '2026-09-10T14:15:41.185711659Z 127.0.0.1 - - [10/Sep/2026:14:15:41 +0000] "GET / HTTP/1.1" 200 182 "-" "Wget" "-"',
        delay: 40,
      },
    ],
  },
];

export interface Feature {
  name: string;
  command: string;
  description: string;
  href: string;
}

export const features: Feature[] = [
  {
    name: "Link",
    command: "coolship link",
    description:
      "Bind the repository to its Coolify application in coolship.toml. The file holds no credentials, so you commit it.",
    href: "/docs/commands/link",
  },
  {
    name: "Deploy",
    command: "coolship deploy",
    description:
      "Start one deployment and tick off its stages until that deployment finishes. The build log prints if it fails.",
    href: "/docs/commands/deploy",
  },
  {
    name: "Env",
    command: "coolship env pull",
    description:
      "Pull, diff, and push .env for one variable scope. Coolship never fills in a value Coolify withholds.",
    href: "/docs/commands/env",
  },
  {
    name: "Dev",
    command: "coolship dev -- npm run dev",
    description:
      "Run a local command with the application's runtime variables set on top of your environment.",
    href: "/docs/commands/dev",
  },
];

export interface Step {
  title: string;
  command: string;
  description: string;
  href: string;
}

export const minute: Step[] = [
  {
    title: "Log in once",
    command: "coolship login",
    description:
      "Coolship checks the URL and token against the server and stores them in the file coolify-cli uses, so logging in with either tool logs you in to both.",
    href: "/docs/commands/login",
  },
  {
    title: "Link the repository",
    command: "coolship link",
    description:
      "Coolship picks the project, environment, and application, and asks only when there is more than one to choose from. It writes the result to coolship.toml, which every later command reads.",
    href: "/docs/commands/link",
  },
  {
    title: "Deploy",
    command: "coolship deploy",
    description:
      "Coolship deploys the source and branch set in Coolify and waits for that deployment, ticking off each stage as it finishes.",
    href: "/docs/commands/deploy",
  },
  {
    title: "Pull the variables",
    command: "coolship env pull",
    description:
      "Coolship writes the application's variables into .env and keeps your local-only keys and comments. A value Coolify withholds becomes a comment, not an empty entry.",
    href: "/docs/commands/env",
  },
  {
    title: "Preview a pull request",
    command: "coolship preview --pr 42",
    description:
      "Coolship deploys the preview Coolify has for the pull request and waits for it the way deploy does. In a GitHub Actions pull_request job, it reads the number from GITHUB_REF.",
    href: "/docs/commands/preview",
  },
];

export interface ComparisonRow {
  task: string;
  coolify: string[];
  coolship: string[];
  note?: string;
  /** Rows where coolify-cli is the tool to reach for. */
  theirs?: boolean;
}

export const comparison: ComparisonRow[] = [
  {
    task: "Deploy",
    coolify: ["coolify deploy uuid <application-uuid>"],
    coolship: ["coolship deploy"],
    note: "Coolship waits for the deployment it started and shows its stages as they finish.",
  },
  {
    task: "Logs",
    coolify: ["coolify app logs <uuid> --follow"],
    coolship: ["coolship logs --follow"],
    note: "Coolship polls log snapshots and warns when lines may be missing.",
  },
  {
    task: "Variables",
    coolify: [
      "coolify app env list <app-uuid>",
      "coolify app env sync <app-uuid>",
    ],
    coolship: ["coolship env pull", "coolship env diff", "coolship env push"],
    note: "Coolship works on one scope at a time and never fills in a withheld value.",
  },
  {
    task: "Status",
    coolify: ["coolify app get <uuid>"],
    coolship: ["coolship status"],
  },
  {
    task: "Pull request preview",
    coolify: ["coolify deploy uuid <uuid> --pull-request-id 42"],
    coolship: ["coolship preview --pr 42"],
  },
  {
    task: "Servers, keys, teams",
    coolify: ["coolify server …", "coolify private-key …", "coolify teams …"],
    coolship: [],
    note: "Coolship does not administer the instance. Use coolify-cli for these.",
    theirs: true,
  },
];

export interface BuiltIn {
  value: string;
  label: string;
  title: string;
  description: string;
  href: string;
  /** The link's text; names the page so it reads on its own. */
  linkLabel: string;
  samples: Array<{ lang: Lang; title?: string; code: string }>;
}

export const builtIns: BuiltIn[] = [
  {
    value: "monorepo",
    label: "Monorepo targets",
    title: "Several applications, one file",
    description:
      "A repository with several applications uses named targets instead of [project]. Commands pick the target whose root contains the current directory, or take its name.",
    href: "/docs/guides/monorepos",
    linkLabel: "Monorepos guide",
    samples: [
      {
        lang: "toml",
        title: "coolship.toml",
        code: `version = 1

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
root = "apps/api"`,
      },
      {
        lang: "bash",
        code: `cd apps/api && coolship deploy   # the target whose root contains the directory
coolship deploy api              # or name it from anywhere
coolship logs web --follow`,
      },
    ],
  },
  {
    value: "previews",
    label: "Previews",
    title: "Pull request previews, from the terminal or CI",
    description:
      "Coolify must already know about the pull request. Turn on Preview Deployments and let Coolify's GitHub webhook register it. preview then deploys that preview and waits for it the way deploy does.",
    href: "/docs/concepts/previews",
    linkLabel: "Preview deployments",
    samples: [
      {
        lang: "bash",
        code: `coolship preview --pr 42
coolship preview api --pr 42   # a named monorepo target, like deploy
coolship preview               # in a GitHub Actions pull_request job, reads GITHUB_REF`,
      },
      {
        lang: "yaml",
        title: ".github/workflows/preview.yml",
        code: `on:
  pull_request:

jobs:
  preview:
    runs-on: ubuntu-latest
    env:
      COOLSHIP_URL: \${{ secrets.COOLSHIP_URL }}
      COOLSHIP_TOKEN: \${{ secrets.COOLSHIP_TOKEN }}
    steps:
      - uses: actions/checkout@v4
      - name: Install Coolship
        run: |
          curl -fsSL https://raw.githubusercontent.com/joaomnuno/coolship/main/scripts/install.sh | sh
          echo "$HOME/.local/bin" >> "$GITHUB_PATH"
      # The pull request number is read from GITHUB_REF; --pr overrides it.
      - name: Deploy the preview
        run: coolship preview`,
      },
    ],
  },
  {
    value: "doctor",
    label: "doctor",
    title: "Check the setup before a command fails",
    description:
      "doctor checks the project configuration, the Git repository, the binding, and your credentials and context. It then confirms the server responds, reports its version, and checks that the binding points at a running application. It exits with status 1 when any check fails.",
    href: "/docs/commands/doctor",
    linkLabel: "doctor reference",
    samples: [
      {
        lang: "text",
        title: "coolship doctor",
        code: `$ coolship doctor
[ok]   Project configuration: /home/you/my-app/coolship.toml
[ok]   Git repository: /home/you/my-app
[ok]   Binding: Personal / production / fenix-bot in /home/you/my-app
[ok]   Credentials: /home/you/.config/coolify/config.json (1 instance, default home)
[ok]   Context: home at https://coolify.example.com
[ok]   Server: Coolify 4.3.18
[ok]   Application: fenix-bot (9f8e7d6c) is running:healthy`,
      },
    ],
  },
  {
    value: "dev",
    label: "dev",
    title: "Run locally with the application's variables",
    description:
      "dev sets the application's runtime variables on top of your environment and resolves shared references, so the process gets the values it gets on Coolify. You don't need to pull a .env first. Coolship exits with the command's exit status.",
    href: "/docs/commands/dev",
    linkLabel: "dev reference",
    samples: [
      {
        lang: "bash",
        code: `coolship dev -- npm run dev
coolship dev api -- go run .
coolship dev                       # runs the binding's dev setting`,
      },
      {
        lang: "toml",
        title: "coolship.toml",
        code: `[project]
dev = "npm run dev"`,
      },
    ],
  },
  {
    value: "domain",
    label: "domain",
    title: "Show and replace the application's domains",
    description:
      "Coolify generates a domain from the application UUID until you set your own. domain set shows the change and asks before applying it. It refuses a domain used elsewhere on the server unless you pass --force, then reads the application back to show which domains Coolify kept.",
    href: "/docs/commands/domain",
    linkLabel: "domain reference",
    samples: [
      {
        lang: "bash",
        code: `coolship domain
coolship domain set app.example.com                       # bare host means https://
coolship domain set https://app.example.com https://www.example.com --redirect non-www`,
      },
      {
        lang: "json",
        title: "coolship domain set … --format json",
        code: `{
  "plan": {
    "target": { "…": "the resolved target" },
    "current": ["https://9f8e7d6c.coolify.example.com"],
    "domains": ["https://app.example.com", "https://www.example.com"],
    "redirect": "non-www"
  },
  "warnings": ["The proxy learns the new domains on the next deployment."]
}`,
      },
    ],
  },
  {
    value: "json",
    label: "JSON output",
    title: "One result object per command",
    description:
      "Results go to stdout and progress goes to stderr, so you can pipe --format json output into jq or a script. Every result that names an application has the same target object. logs prints one JSON event per line.",
    href: "/docs/platform/output",
    linkLabel: "Output and exit codes",
    samples: [
      {
        lang: "json",
        title: "coolship status --format json",
        code: `{
  "target": {
    "target": "default",
    "instance": "home",
    "instance_url": "https://coolify.example.com",
    "project": "coolship-example",
    "project_uuid": "rxv3lqhdvuprnl433dczvo0s",
    "environment": "production",
    "environment_uuid": "5omkp5uuj0qpet6dy16r6bag",
    "application": "coolship-example",
    "application_uuid": "mm4c0zpbrzx8z96t0qiw3tff",
    "root": "/home/you/my-app"
  },
  "status": "running:healthy",
  "url": "https://coolship.example.com"
}`,
      },
      {
        lang: "json",
        title: "coolship logs --format json",
        code: `{"type":"logs","logs":"2026-09-10T11:49:33.442427921Z 127.0.0.1 - - [10/Sep/2026:11:49:33 +0000] \\"GET / HTTP/1.1\\" 200 182 \\"-\\" \\"Wget\\" \\"-\\"\\n"}`,
      },
    ],
  },
];
