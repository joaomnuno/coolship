import Link from "next/link";
import { ExternalLink } from "lucide-react";
import { CopyButton } from "@/components/copy-button";
import { installCommand, installScriptUrl } from "@/lib/site";
import { cn } from "./cn";
import { TerminalFrame, Tokens } from "./code";
import { tokenizeLine } from "./highlight";

/** The install one-liner: highlighted, copyable, with the two links bun.com pairs it with. */
export function InstallBlock({
  id,
  className,
}: {
  id?: string;
  className?: string;
}) {
  return (
    <div id={id} className={cn("scroll-mt-28", className)}>
      <TerminalFrame
        title="Linux & macOS"
        aside={
          <span className="text-fd-muted-foreground/70">amd64 and arm64</span>
        }
        className="install-frame"
      >
        <div className="flex items-stretch">
          <pre className="m-0 min-w-0 flex-1 overflow-x-auto px-4 py-3.5 font-mono text-[0.84rem] leading-relaxed sm:text-[0.9rem]">
            <code>
              <Tokens tokens={tokenizeLine(installCommand, "bash")} />
            </code>
          </pre>
          <CopyButton
            text={installCommand}
            label="Copy"
            className="term-bar flex shrink-0 items-center gap-1.5 border-l border-white/[0.06] px-3.5 text-xs font-medium transition-colors hover:bg-fd-primary hover:text-fd-primary-foreground focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-fd-primary"
          />
        </div>
      </TerminalFrame>
      <p className="mt-3 flex flex-wrap items-center gap-x-5 gap-y-1 text-sm text-fd-muted-foreground">
        <a
          href={installScriptUrl}
          target="_blank"
          rel="noreferrer noopener"
          className="inline-flex items-center gap-1 underline-offset-4 hover:text-fd-foreground hover:underline"
        >
          View install script
          <ExternalLink className="size-3.5" aria-hidden="true" />
        </a>
        <Link
          href="/docs/get-started"
          className="inline-flex items-center gap-1 underline-offset-4 hover:text-fd-foreground hover:underline"
        >
          Then follow the quickstart
        </Link>
      </p>
    </div>
  );
}
