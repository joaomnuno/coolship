import Link from "next/link";
import { Check, ShieldCheck } from "lucide-react";
import { Logo } from "@/components/logo";
import { buttonClass } from "@/components/landing/button";
import { CodeBlock } from "@/components/landing/code";
import {
  builtIns,
  comparison,
  features,
  minute,
  replayScript,
} from "@/components/landing/content";
import { InstallBlock } from "@/components/landing/install-block";
import { Heading, Lede, Section } from "@/components/landing/section";
import { Tabs } from "@/components/landing/tabs";
import { TerminalReplay } from "@/components/landing/terminal-replay";
import { VideoSlot } from "@/components/landing/video-slot";
import {
  latestVersion,
  repo,
  showcasePoster,
  showcaseVideoUrl,
  siteName,
  verifiedCoolifyVersion,
} from "@/lib/site";

function Hero() {
  return (
    <section
      aria-labelledby="hero-title"
      className="hero relative overflow-hidden"
    >
      <div className="mx-auto flex w-full max-w-6xl flex-col items-center px-4 pt-20 pb-16 text-center sm:px-6 sm:pt-28 sm:pb-20">
        <p className="rise inline-flex items-center gap-2 rounded-full border border-fd-border bg-fd-card/60 py-1 pr-3 pl-1.5 font-mono text-xs text-fd-muted-foreground">
          <span className="rounded-full bg-fd-primary/15 px-2 py-0.5 font-semibold text-fd-primary">
            {latestVersion}
          </span>
          A project-local CLI for Coolify
        </p>
        <h1
          id="hero-title"
          className="rise rise-1 mt-6 max-w-4xl text-5xl font-extrabold tracking-[-0.03em] text-balance sm:text-7xl lg:text-[5.5rem] lg:leading-[1.02]"
        >
          Link once, then ship.
        </h1>
        <p className="rise rise-2 mt-6 max-w-2xl text-lg text-pretty text-fd-muted-foreground sm:text-xl">
          Link a repository to its Coolify application once, then deploy, tail
          logs, sync variables, and run locally from the terminal, without
          UUIDs.
        </p>
        <div className="rise rise-3 mt-8 flex flex-wrap items-center justify-center gap-3">
          <a href="#install" className={buttonClass("primary", "lg")}>
            Install {latestVersion}
          </a>
          <Link
            href="/docs/get-started"
            className={buttonClass("secondary", "lg")}
          >
            Quickstart
          </Link>
        </div>
        <InstallBlock
          id="install"
          className="rise rise-4 mt-12 w-full max-w-2xl text-left"
        />
      </div>
    </section>
  );
}

function Showcase() {
  return (
    <Section aria-labelledby="showcase-title" className="pt-4 sm:pt-6">
      <div className="mx-auto max-w-4xl">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <div>
            <Heading id="showcase-title">See Coolship in action</Heading>
          </div>
          <p className="max-w-sm text-sm text-fd-muted-foreground">
            From an empty terminal to a deployed, linked application, with the
            build log streaming.
          </p>
        </div>
        <div className="mt-8">
          <VideoSlot
            src={showcaseVideoUrl}
            poster={showcasePoster}
            title="See Coolship in action"
          />
        </div>
      </div>
    </Section>
  );
}

function Replay() {
  return (
    <Section aria-labelledby="replay-title">
      <div className="grid gap-10 lg:grid-cols-[minmax(0,2fr)_minmax(0,3fr)] lg:items-center">
        <div>
          <Heading id="replay-title">The whole loop, one binding</Heading>
          <Lede>
            After <code className="font-mono text-fd-foreground">link</code>, no
            command needs a resource identifier.{" "}
            <code className="font-mono text-fd-foreground">deploy</code> submits
            one deployment and follows the UUID that submission returned,
            relaying the server&apos;s build log while it waits.
          </Lede>
          <ul className="mt-6 space-y-3 text-sm text-fd-muted-foreground">
            {[
              "Interrupting stops local waiting only; the deployment continues and its UUID is reported.",
              "Results go to stdout, progress and prompts to stderr, so piping stays useful.",
              "This replay is a real session against the example application, typed back at typing speed.",
            ].map((item) => (
              <li key={item} className="flex gap-3">
                <Check
                  className="mt-0.5 size-4 shrink-0 text-fd-primary"
                  aria-hidden="true"
                />
                <span>{item}</span>
              </li>
            ))}
          </ul>
        </div>
        <div className="min-w-0">
          <TerminalReplay script={replayScript} title="my-app" />
        </div>
      </div>
    </Section>
  );
}

function Features() {
  return (
    <Section aria-labelledby="features-title">
      <div>
        <Heading id="features-title">Four verbs, one project</Heading>
        <Lede>
          Everything reads the binding that link wrote. Every command takes
          --format json.
        </Lede>
      </div>
      <ul className="mt-10 grid gap-px overflow-hidden rounded-2xl border border-fd-border bg-fd-border sm:grid-cols-2 lg:grid-cols-4">
        {features.map((feature) => (
          <li
            key={feature.name}
            className="group relative flex flex-col gap-4 bg-fd-card p-6 transition-colors hover:bg-fd-accent/40"
          >
            <h3 className="text-xl font-semibold tracking-tight">
              {feature.name}
            </h3>
            <p className="text-sm text-pretty text-fd-muted-foreground">
              {feature.description}
            </p>
            <p className="mt-auto font-mono text-sm text-fd-foreground">
              <span className="tk-prompt">$ </span>
              <span className="text-fd-primary">{feature.command}</span>
            </p>
            <Link
              href={feature.href}
              className="inline-flex items-center gap-1 text-sm font-medium text-fd-muted-foreground underline-offset-4 after:absolute after:inset-0 hover:text-fd-foreground focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-fd-primary group-hover:underline"
            >
              {feature.name === "Env"
                ? "env pull, diff, and push"
                : `coolship ${feature.name.toLowerCase()}`}{" "}
              docs
            </Link>
          </li>
        ))}
      </ul>
    </Section>
  );
}

function Minute() {
  return (
    <Section aria-labelledby="minute-title">
      <div>
        <Heading id="minute-title">A minute with Coolship</Heading>
        <Lede>
          Five commands from a fresh terminal to a pull-request preview.
        </Lede>
      </div>
      <ol className="mt-10 grid gap-x-8 gap-y-2 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
        {minute.map((step, index) => (
          <li
            key={step.command}
            className="flex gap-5 border-t border-fd-border py-6 lg:[&:nth-child(2)]:border-t"
          >
            <span
              className="mt-1 font-mono text-sm font-semibold text-fd-primary tabular-nums"
              aria-hidden="true"
            >
              {String(index + 1).padStart(2, "0")}
            </span>
            <div className="min-w-0 flex-1">
              <h3 className="text-lg font-semibold tracking-tight">
                <span className="sr-only">Step {index + 1}: </span>
                {step.title}
              </h3>
              <pre className="mt-2 overflow-x-auto font-mono text-[0.95rem] text-fd-foreground">
                <code>
                  <span className="tk-prompt">$ </span>
                  {step.command}
                </code>
              </pre>
              <p className="mt-2 text-sm text-pretty text-fd-muted-foreground">
                {step.description}
              </p>
              <Link
                href={step.href}
                className="mt-2 inline-flex items-center gap-1 text-sm font-medium text-fd-primary underline-offset-4 hover:underline"
              >
                {step.command.split(" ")[1]} reference
              </Link>
            </div>
          </li>
        ))}
      </ol>
    </Section>
  );
}

function Comparison() {
  return (
    <Section aria-labelledby="comparison-title">
      <div>
        <Heading id="comparison-title">
          Complementary to coolify-cli, not a replacement
        </Heading>
        <Lede>
          <a
            href="https://github.com/coollabsio/coolify-cli"
            className="font-mono text-fd-foreground underline underline-offset-4"
          >
            coolify-cli
          </a>{" "}
          manages Coolify resources. Coolship manages the developer workflow
          around the project you are in. They share one login: both read the
          same credentials file.
        </Lede>
      </div>
      <div className="mt-10 overflow-x-auto rounded-2xl border border-fd-border">
        <table className="w-full min-w-[40rem] border-collapse text-sm">
          <thead>
            <tr className="bg-fd-card text-left">
              <th scope="col" className="px-5 py-3.5 font-semibold">
                Task
              </th>
              <th
                scope="col"
                className="px-5 py-3.5 font-semibold text-fd-muted-foreground"
              >
                with coolify-cli
              </th>
              <th
                scope="col"
                className="px-5 py-3.5 font-semibold text-fd-primary"
              >
                with {siteName}
              </th>
            </tr>
          </thead>
          <tbody>
            {comparison.map((row) => (
              <tr
                key={row.task}
                className="border-t border-fd-border align-top"
              >
                <th scope="row" className="px-5 py-4 text-left font-medium">
                  {row.task}
                </th>
                <td className="px-5 py-4">
                  <ul className="space-y-1 font-mono text-[0.8rem] text-fd-muted-foreground">
                    {row.coolify.map((command) => (
                      <li key={command}>{command}</li>
                    ))}
                  </ul>
                </td>
                <td className="px-5 py-4">
                  {row.coolship.length > 0 && (
                    <ul className="space-y-1 font-mono text-[0.8rem] text-fd-foreground">
                      {row.coolship.map((command) => (
                        <li key={command}>
                          <span className="tk-prompt">$ </span>
                          {command}
                        </li>
                      ))}
                    </ul>
                  )}
                  {row.note && (
                    <p
                      className={
                        row.theirs
                          ? "text-fd-muted-foreground"
                          : "mt-1.5 text-xs text-fd-muted-foreground"
                      }
                    >
                      {row.note}
                    </p>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Section>
  );
}

function BuiltIns() {
  return (
    <Section aria-labelledby="builtins-title">
      <div>
        <Heading id="builtins-title">The parts around deploy</Heading>
        <Lede>
          Real samples from the reference. Each one is a command you can run
          today.
        </Lede>
      </div>
      <Tabs
        label="Built-in features"
        className="mt-8"
        items={builtIns.map((item) => ({
          value: item.value,
          label: item.label,
          content: (
            <div className="grid gap-6 lg:grid-cols-[minmax(0,2fr)_minmax(0,3fr)] lg:gap-10">
              <div>
                <h3 className="text-xl font-semibold tracking-tight">
                  {item.title}
                </h3>
                <p className="mt-3 text-sm text-pretty text-fd-muted-foreground">
                  {item.description}
                </p>
                <Link
                  href={item.href}
                  className="mt-4 inline-flex items-center gap-1 text-sm font-medium text-fd-primary underline-offset-4 hover:underline"
                >
                  {item.linkLabel}
                </Link>
              </div>
              <div className="flex min-w-0 flex-col gap-4">
                {item.samples.map((sample, i) => (
                  <CodeBlock
                    key={i}
                    code={sample.code}
                    lang={sample.lang}
                    title={sample.title}
                  />
                ))}
              </div>
            </div>
          ),
        }))}
      />
    </Section>
  );
}

function Verified() {
  return (
    <div className="mx-auto w-full max-w-6xl px-4 sm:px-6">
      <div className="flex flex-wrap items-center justify-between gap-4 rounded-2xl border border-fd-primary/25 bg-fd-primary/[0.06] px-6 py-5">
        <p className="flex items-start gap-3">
          <ShieldCheck
            className="mt-0.5 size-5 shrink-0 text-fd-primary"
            aria-hidden="true"
          />
          <span>
            <strong className="font-semibold">
              Verified against Coolify {verifiedCoolifyVersion}.
            </strong>{" "}
            <span className="text-fd-muted-foreground">
              Every command was run end to end against a live instance; 4.3.19
              changes none of the endpoints Coolship uses.
            </span>
          </span>
        </p>
        <Link
          href="/docs/concepts/server-compatibility"
          className="inline-flex items-center gap-1 text-sm font-medium text-fd-primary underline-offset-4 hover:underline"
        >
          What was verified
        </Link>
      </div>
    </div>
  );
}

function ClosingCta() {
  return (
    <Section aria-labelledby="cta-title" className="pb-24 sm:pb-32">
      <div className="relative overflow-hidden rounded-3xl border border-fd-border bg-fd-card px-6 py-14 text-center sm:px-12 sm:py-20">
        <div
          className="cta-glow pointer-events-none absolute inset-0"
          aria-hidden="true"
        />
        <div className="relative mx-auto flex max-w-2xl flex-col items-center">
          <Heading id="cta-title" className="sm:text-5xl">
            Try it on the repository you have open right now
          </Heading>
          <Lede className="text-center">
            One line installs {latestVersion} into{" "}
            <code className="font-mono">~/.local/bin</code>, checksum verified,
            never with sudo. Then{" "}
            <code className="font-mono">coolship link</code>.
          </Lede>
          <InstallBlock className="mt-8 w-full text-left" />
          <div className="mt-8 flex flex-wrap items-center justify-center gap-3">
            <Link
              href="/docs/get-started"
              className={buttonClass("primary", "lg")}
            >
              Read the quickstart
            </Link>
            <a
              href={`${repo.url}/releases`}
              className={buttonClass("secondary", "lg")}
              target="_blank"
              rel="noreferrer noopener"
            >
              Releases
            </a>
          </div>
        </div>
      </div>
    </Section>
  );
}

const footerColumns: Array<{
  title: string;
  links: Array<{ label: string; href: string; external?: boolean }>;
}> = [
  {
    title: "Product",
    links: [
      { label: "Docs", href: "/docs" },
      { label: "Guides", href: "/docs/guides/ci" },
      {
        label: "Changelog",
        href: `${repo.url}/blob/${repo.branch}/CHANGELOG.md`,
        external: true,
      },
      { label: "Releases", href: `${repo.url}/releases`, external: true },
    ],
  },
  {
    title: "Commands",
    links: [
      { label: "link", href: "/docs/commands/link" },
      { label: "deploy", href: "/docs/commands/deploy" },
      { label: "logs", href: "/docs/commands/logs" },
      { label: "env", href: "/docs/commands/env" },
      { label: "preview", href: "/docs/commands/preview" },
      { label: "doctor", href: "/docs/commands/doctor" },
    ],
  },
  {
    title: "Community",
    links: [
      { label: "GitHub", href: repo.url, external: true },
      { label: "Issues", href: `${repo.url}/issues`, external: true },
      { label: "Coolify", href: "https://coolify.io/", external: true },
    ],
  },
  {
    title: "Project",
    links: [
      {
        label: "License (MIT)",
        href: `${repo.url}/blob/${repo.branch}/LICENSE`,
        external: true,
      },
      { label: "Source", href: repo.url, external: true },
      {
        label: "Server compatibility",
        href: "/docs/concepts/server-compatibility",
      },
      { label: "llms.txt", href: "/llms.txt" },
    ],
  },
];

function SiteFooter() {
  return (
    <footer className="border-t border-fd-border">
      <div className="mx-auto grid w-full max-w-6xl gap-10 px-4 py-14 sm:px-6 md:grid-cols-[1.4fr_repeat(4,minmax(0,1fr))]">
        <div className="max-w-xs">
          <Link href="/" className="inline-flex items-center gap-2">
            <Logo className="size-6" />
            <span className="font-semibold tracking-tight">{siteName}</span>
          </Link>
          <p className="mt-3 text-sm font-medium">Link once, then ship.</p>
          <p className="mt-1 text-sm text-fd-muted-foreground">
            A project-local developer CLI for Coolify, written in Go and
            released under the MIT license.
          </p>
        </div>
        {footerColumns.map((column) => (
          <nav key={column.title} aria-label={column.title}>
            <h2 className="text-sm font-semibold">{column.title}</h2>
            <ul className="mt-3 space-y-2 text-sm">
              {column.links.map((link) => (
                <li key={link.label}>
                  {link.external ? (
                    <a
                      href={link.href}
                      target="_blank"
                      rel="noreferrer noopener"
                      className="text-fd-muted-foreground hover:text-fd-foreground"
                    >
                      {link.label}
                    </a>
                  ) : (
                    <Link
                      href={link.href}
                      className={`text-fd-muted-foreground hover:text-fd-foreground ${column.title === "Commands" ? "font-mono" : ""}`}
                    >
                      {link.label}
                    </Link>
                  )}
                </li>
              ))}
            </ul>
          </nav>
        ))}
      </div>
      <div className="mx-auto flex w-full max-w-6xl flex-wrap items-center justify-between gap-2 border-t border-fd-border px-4 py-5 text-xs text-fd-muted-foreground sm:px-6">
        <p>
          © {new Date().getFullYear()} João Nuno. Coolship is an independent
          project, not affiliated with Coolify.
        </p>
        <p>Verified against Coolify {verifiedCoolifyVersion}.</p>
      </div>
    </footer>
  );
}

export default function HomePage() {
  return (
    <>
      <main id="main" className="landing flex-1">
        <Hero />
        <Showcase />
        <Replay />
        <Features />
        <Minute />
        <Comparison />
        <BuiltIns />
        <Verified />
        <ClosingCta />
      </main>
      <SiteFooter />
    </>
  );
}
