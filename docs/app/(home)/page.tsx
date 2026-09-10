import Link from 'next/link';
import { ArrowRight } from 'lucide-react';
import { CopyButton } from '@/components/copy-button';
import { installCommand, repo } from '@/lib/site';

const commands: Array<[string, string]> = [
  ['init', 'Create a Coolify application from the repository’s public remote, then link it.'],
  ['link', 'Bind the repository to an existing application and write a credential-free coolship.toml.'],
  ['status', 'Report the linked application’s current status and URL.'],
  ['deploy', 'Deploy from the source configured in Coolify, wait for that exact deployment, and stream its build log.'],
  ['logs', 'Read runtime logs; --follow keeps polling and reports gaps instead of hiding them.'],
  ['open', 'Open the application, or its Coolify page with --dashboard; --print only prints the URL.'],
  ['doctor', 'Run every step a command performs and report each one, exiting 1 on a failure.'],
  ['env pull · diff · push', 'Synchronize .env with one scope of the application’s variables, never inventing withheld values.'],
  ['preview', 'Deploy the preview Coolify already holds for a pull request, from --pr or GITHUB_REF.'],
  ['dev', 'Run a local command with the application’s runtime variables injected, exit status forwarded.'],
  ['domain', 'Show the application’s domains, or replace them with domain set.'],
  ['login', 'Verify a Coolify URL and token, then store them in the file coolify-cli uses.'],
];

function Terminal({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <figure className="terminal my-0 overflow-hidden rounded-lg text-[0.84rem] leading-relaxed">
      <figcaption className="terminal-bar flex items-center gap-2 px-4 py-2 font-mono text-xs">
        <span className="size-2 rounded-full bg-emerald-400" aria-hidden="true" />
        {title}
      </figcaption>
      <pre className="m-0 overflow-x-auto px-4 py-4 font-mono">{children}</pre>
    </figure>
  );
}

export default function HomePage() {
  return (
    <main className="mx-auto w-full max-w-5xl px-4 pb-24 sm:px-6">
      <section className="pt-16 sm:pt-24">
        <p className="font-mono text-xs uppercase tracking-[0.08em] text-fd-muted-foreground">
          Coolship
        </p>
        <h1 className="mt-3 max-w-3xl text-4xl font-bold tracking-tight text-balance sm:text-6xl">
          Link once, then ship.
        </h1>
        <p className="mt-5 max-w-2xl text-lg text-fd-muted-foreground text-pretty">
          Coolship is a project-local CLI for{' '}
          <a href="https://coolify.io/" className="text-fd-primary underline underline-offset-4">
            Coolify
          </a>
          . It binds the repository you are in to its Coolify application, so{' '}
          <code className="rounded bg-fd-muted px-1 py-0.5 font-mono text-[0.9em]">deploy</code>,{' '}
          <code className="rounded bg-fd-muted px-1 py-0.5 font-mono text-[0.9em]">logs</code>, and{' '}
          <code className="rounded bg-fd-muted px-1 py-0.5 font-mono text-[0.9em]">env</code> work from
          the terminal without UUIDs or dashboard navigation.
        </p>

        <div className="mt-8 flex flex-wrap items-center gap-3">
          <Link
            href="/docs/get-started"
            className="inline-flex items-center gap-2 rounded-md bg-fd-primary px-4 py-2 text-sm font-medium text-fd-primary-foreground hover:bg-fd-primary/90"
          >
            Get started
            <ArrowRight className="size-4" />
          </Link>
          <Link
            href="/docs"
            className="inline-flex items-center rounded-md border px-4 py-2 text-sm font-medium hover:bg-fd-accent"
          >
            Read the docs
          </Link>
        </div>

        <div className="mt-10">
          <p className="font-mono text-xs uppercase tracking-[0.08em] text-fd-muted-foreground">
            Install on Linux or macOS
          </p>
          <div className="terminal mt-2 flex items-stretch overflow-hidden rounded-lg">
            <pre className="m-0 min-w-0 flex-1 overflow-x-auto px-4 py-3 font-mono text-[0.86rem] leading-relaxed">
              <code>{installCommand}</code>
            </pre>
            <CopyButton
              text={installCommand}
              className="terminal-bar flex shrink-0 items-center gap-1.5 border-l border-white/5 px-3 text-xs hover:bg-fd-primary hover:text-fd-primary-foreground"
            />
          </div>
          <p className="mt-3 text-sm text-fd-muted-foreground">
            The script verifies the release checksum and installs into{' '}
            <code className="font-mono">$HOME/.local/bin</code>, never with sudo. Or{' '}
            <Link href="/docs/get-started#build-from-source" className="underline underline-offset-4">
              build from source
            </Link>{' '}
            with Go 1.26 or newer.
          </p>
        </div>
      </section>

      <section className="mt-20 grid gap-8 lg:grid-cols-[1fr_1.2fr] lg:items-start">
        <div>
          <p className="font-mono text-xs uppercase tracking-[0.08em] text-fd-muted-foreground">
            In the terminal
          </p>
          <h2 className="mt-2 text-2xl font-semibold tracking-tight">One deployment, observed exactly</h2>
          <p className="mt-3 text-fd-muted-foreground">
            After <code className="font-mono">coolship link</code>, no command needs a resource
            identifier. <code className="font-mono">deploy</code> submits one deployment and follows
            the UUID that submission returned, streaming the server&apos;s build log while it waits.
            Interrupting stops local waiting only; the deployment continues and its UUID is reported.
          </p>
          <p className="mt-3 text-fd-muted-foreground">
            When something is off, <code className="font-mono">doctor</code> runs every step a command
            performs and reports each one.
          </p>
        </div>
        <div className="flex flex-col gap-4">
          <Terminal title="coolship deploy">
            <span className="prompt">$</span> coolship deploy{'\n'}
            Deployment 03dusayin5rleswixblvdqba: queued{'\n'}
            Deployment 03dusayin5rleswixblvdqba: in_progress{'\n'}
            <span className="dim">
              Starting deployment of joaomnuno/example-coolify-project:main to Master Ubuntu.{'\n'}
              Building docker image started.{'\n'}
              Building docker image completed.{'\n'}
              Rolling update started.{'\n'}
              Attempt 2 of 10 | Healthcheck status: &quot;healthy&quot;{'\n'}
              Rolling update completed.
            </span>
            {'\n'}
            Deployment 03dusayin5rleswixblvdqba: <span className="ok">finished</span>{'\n'}
            Deployment: 03dusayin5rleswixblvdqba{'\n'}
            Application: coolship-example (mm4c0zpbrzx8z96t0qiw3tff){'\n'}
            Status: <span className="ok">finished</span>
          </Terminal>
          <Terminal title="coolship doctor">
            <span className="prompt">$</span> coolship doctor{'\n'}
            <span className="ok">[ok]</span>   Project configuration: /home/you/my-app/coolship.toml{'\n'}
            <span className="ok">[ok]</span>   Git repository: /home/you/my-app{'\n'}
            <span className="ok">[ok]</span>   Binding: Personal / production / fenix-bot in /home/you/my-app{'\n'}
            <span className="ok">[ok]</span>   Credentials: /home/you/.config/coolify/config.json (1 instance, default home){'\n'}
            <span className="ok">[ok]</span>   Context: home at https://coolify.example.com{'\n'}
            <span className="ok">[ok]</span>   Server: Coolify 4.3.18{'\n'}
            <span className="ok">[ok]</span>   Application: fenix-bot (9f8e7d6c) is running:healthy
          </Terminal>
        </div>
      </section>

      <section className="mt-20">
        <p className="font-mono text-xs uppercase tracking-[0.08em] text-fd-muted-foreground">
          Commands
        </p>
        <h2 className="mt-2 text-2xl font-semibold tracking-tight">What it does</h2>
        <p className="mt-3 max-w-2xl text-fd-muted-foreground">
          Everything reads the binding that <code className="font-mono">link</code> wrote. Every command
          takes <code className="font-mono">--format json</code>; results go to stdout, progress and
          prompts to stderr.
        </p>
        <dl className="mt-8 grid gap-x-8 gap-y-5 sm:grid-cols-2">
          {commands.map(([name, description]) => (
            <div key={name} className="flex flex-col gap-1">
              <dt className="font-mono text-sm font-semibold text-fd-primary">{name}</dt>
              <dd className="text-sm text-fd-muted-foreground">{description}</dd>
            </div>
          ))}
        </dl>
        <p className="mt-8">
          <Link
            href="/docs/commands"
            className="inline-flex items-center gap-1 text-sm font-medium text-fd-primary underline underline-offset-4"
          >
            Command reference
            <ArrowRight className="size-4" />
          </Link>
        </p>
      </section>

      <section className="mt-20 grid gap-10 md:grid-cols-2">
        <div>
          <p className="font-mono text-xs uppercase tracking-[0.08em] text-fd-muted-foreground">
            Project binding
          </p>
          <h2 className="mt-2 text-2xl font-semibold tracking-tight">One small file, committed</h2>
          <p className="mt-3 text-fd-muted-foreground">
            <code className="font-mono">link</code> writes a versioned, credential-free file at the
            repository root. Commit it; tokens and secret values are never written to it. Monorepos use{' '}
            <code className="font-mono">[apps.&lt;name&gt;]</code> targets selected by directory or by
            name.
          </p>
          <pre className="mt-4 overflow-x-auto rounded-lg bg-fd-muted p-4 font-mono text-[0.86rem] leading-relaxed">
            {`version = 1

[project]
context = "home"
project = "Personal"
environment = "production"
application = "fenix-bot"
root = "."`}
          </pre>
        </div>
        <div>
          <p className="font-mono text-xs uppercase tracking-[0.08em] text-fd-muted-foreground">
            Scope
          </p>
          <h2 className="mt-2 text-2xl font-semibold tracking-tight">How it relates to coolify-cli</h2>
          <p className="mt-3 text-fd-muted-foreground">
            Coolify already has{' '}
            <a
              href="https://github.com/coollabsio/coolify-cli"
              className="font-mono text-fd-primary underline underline-offset-4"
            >
              coolify-cli
            </a>
            , and Coolship is not meant to replace it. coolify-cli manages Coolify resources; Coolship
            manages the developer workflow around the project you are currently working on. Creating
            servers, private keys, or team members stays out of scope.
          </p>
          <p className="mt-3 text-fd-muted-foreground">
            Credentials are shared: Coolship reads the contexts coolify-cli stores in{' '}
            <code className="font-mono">~/.config/coolify/config.json</code>, and{' '}
            <code className="font-mono">coolship login</code> writes to the same file. In CI,{' '}
            <code className="font-mono">COOLSHIP_URL</code> and{' '}
            <code className="font-mono">COOLSHIP_TOKEN</code> stand in for it.
          </p>
          <aside className="mt-6 rounded-r-lg border-l-[3px] border-fd-primary bg-fd-muted px-4 py-3 text-sm">
            <strong>Verified against Coolify 4.3.18.</strong> Every command was run end to end against
            a live instance. The 4.3.19 source has no changes to any endpoint Coolship uses; other
            versions are untested.{' '}
            <Link href="/docs/platform/limits" className="underline underline-offset-4">
              Known limits
            </Link>
            .
          </aside>
        </div>
      </section>

      <footer className="mt-20 flex flex-wrap gap-x-6 gap-y-2 border-t pt-6 text-sm text-fd-muted-foreground">
        <a href={repo.url} className="hover:text-fd-foreground">
          GitHub
        </a>
        <a href={`${repo.url}/blob/${repo.branch}/CHANGELOG.md`} className="hover:text-fd-foreground">
          Changelog
        </a>
        <a href={`${repo.url}/blob/${repo.branch}/LICENSE`} className="hover:text-fd-foreground">
          MIT license
        </a>
        <Link href="/docs/platform/ai" className="hover:text-fd-foreground">
          llms.txt
        </Link>
      </footer>
    </main>
  );
}
