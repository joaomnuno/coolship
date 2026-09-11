/**
 * Everything the landing page says, in one place. Transcripts are real: the
 * `deploy` and `doctor` output is copied from README.md, and the `link`,
 * `status`, and `logs` output was captured against the example application
 * (coolship-example, a Dockerfile app) with a local binary; only the working
 * directory and the context name were replaced with the README's
 * `/home/you/my-app` and `home`. JSON shapes come from the command reference.
 */
import type { Lang } from "./highlight";
import type { ReplaySegment } from "./terminal-replay";

export const replayScript: ReplaySegment[] = [
  {
    command:
      "coolship link --project coolship-example --environment production --application coolship-example",
    output: [
      { text: "Linked project in /home/you/my-app/coolship.toml", delay: 700 },
      { text: "Application: coolship-example (mm4c0zpbrzx8z96t0qiw3tff)" },
      { text: "Environment: production" },
      { text: "Project: coolship-example" },
      { text: "Context: home" },
    ],
  },
  {
    // The checklist as it stands once the deployment finished; the live
    // view spins and ticks in place, which the replay's append-only frames
    // cannot show.
    command: "coolship deploy",
    output: [
      { text: "→ coolship-example", delay: 500 },
      { text: "→ production" },
      { text: "" },
      { text: "✓ Deployed                      0:52", delay: 1400 },
      { text: "  ✓ build                       0:41", kind: "dim", delay: 200 },
      { text: "  ✓ rolling update              0:08", kind: "dim", delay: 200 },
      { text: "  ✓ container                   0:06", kind: "dim", delay: 200 },
      { text: "  ✓ cleanup                     0:00", kind: "dim", delay: 200 },
      { text: "Deployment: 03dusayin5rleswixblvdqba", delay: 400 },
      { text: "Application: coolship-example (mm4c0zpbrzx8z96t0qiw3tff)" },
      { text: "Status: finished" },
      { text: "https://coolship.example.com" },
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
      "Bind the repository to its Coolify application once: a small, credential-free coolship.toml you commit.",
    href: "/docs/commands/link",
  },
  {
    name: "Deploy",
    command: "coolship deploy",
    description:
      "Submit one deployment, follow exactly that UUID, and stream the build log while it runs.",
    href: "/docs/commands/deploy",
  },
  {
    name: "Env",
    command: "coolship env pull",
    description:
      "Pull, diff, and push .env against one scope of the variables, never inventing withheld values.",
    href: "/docs/commands/env",
  },
  {
    name: "Dev",
    command: "coolship dev -- npm run dev",
    description:
      "Run a local command with the application's runtime variables injected over your environment.",
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
      "Coolship verifies the URL and token against the server, then stores them in the same file coolify-cli uses, so a login in either tool is a login in both.",
    href: "/docs/commands/login",
  },
  {
    title: "Link the repository",
    command: "coolship link",
    description:
      "It walks project, environment, and application, asking only when a choice is genuinely ambiguous, and writes coolship.toml. Every later command reads that binding.",
    href: "/docs/commands/link",
  },
  {
    title: "Deploy",
    command: "coolship deploy",
    description:
      "Deploy the source and branch already configured in Coolify, wait for exactly that deployment, and watch the build log stream while it runs.",
    href: "/docs/commands/deploy",
  },
  {
    title: "Pull the variables",
    command: "coolship env pull",
    description:
      "Write the application's variables into .env, keeping local-only keys and comments, and noting withheld values as comments rather than writing them empty.",
    href: "/docs/commands/env",
  },
  {
    title: "Preview a pull request",
    command: "coolship preview --pr 42",
    description:
      "Deploy the preview Coolify already holds for the pull request and observe it like deploy; in a GitHub Actions pull_request job the number comes from GITHUB_REF.",
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
    note: "Waits for that exact deployment and streams its build log.",
  },
  {
    task: "Logs",
    coolify: ["coolify app logs <uuid> --follow"],
    coolship: ["coolship logs --follow"],
    note: "Polls snapshots and reports a gap instead of hiding it.",
  },
  {
    task: "Variables",
    coolify: [
      "coolify app env list <app-uuid>",
      "coolify app env sync <app-uuid>",
    ],
    coolship: ["coolship env pull", "coolship env diff", "coolship env push"],
    note: "One scope at a time; withheld values are never invented.",
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
    note: "Out of scope on purpose. coolify-cli remains the right tool for administering the instance.",
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
      "Coolify must already know the pull request (enable Preview Deployments and let its GitHub webhook register it); preview deploys the preview it holds and observes it exactly like deploy.",
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
    title: "Every step a command performs, reported",
    description:
      "Configuration, Git boundary, binding, credentials, context, server reachability and version, and whether the binding resolves to a running application. Exit status 1 when any check fails.",
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
      "The runtime variables are injected over your environment, shared references resolved, so a process sees what it would see on Coolify without pulling a .env first. The child's exit status becomes Coolship's.",
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
      "Coolify generates a domain from the application UUID until you set your own. domain set shows the change, asks first, refuses a domain in use elsewhere unless forced, and reads the application back to confirm what the server kept.",
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
      "Results go to stdout and progress to stderr, so --format json composes with jq and scripts. Every result that names an application carries the same target object; logs prints newline-delimited events.",
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
