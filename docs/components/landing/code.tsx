import type { ReactNode } from "react";
import { cn } from "./cn";
import { tokenize, type Lang, type Token } from "./highlight";

export function Tokens({ tokens }: { tokens: Token[] }) {
  return tokens.map((token, i) =>
    token.cls ? (
      <span key={i} className={token.cls}>
        {token.text}
      </span>
    ) : (
      token.text
    ),
  );
}

/** The window chrome shared by code blocks, transcripts, and the replay. */
export function TerminalFrame({
  title,
  aside,
  className,
  children,
}: {
  title?: ReactNode;
  aside?: ReactNode;
  className?: string;
  children: ReactNode;
}) {
  return (
    <figure className={cn("term overflow-hidden rounded-xl", className)}>
      {(title || aside) && (
        <figcaption className="term-bar flex items-center gap-3 px-4 py-2.5 font-mono text-xs">
          <span className="flex gap-1.5" aria-hidden="true">
            <span className="size-2.5 rounded-full bg-white/10" />
            <span className="size-2.5 rounded-full bg-white/10" />
            <span className="size-2.5 rounded-full bg-white/10" />
          </span>
          {title && <span className="truncate">{title}</span>}
          {aside && (
            <span className="ml-auto flex shrink-0 items-center gap-2">
              {aside}
            </span>
          )}
        </figcaption>
      )}
      {children}
    </figure>
  );
}

/** A syntax-highlighted, read-only code sample. */
export function CodeBlock({
  code,
  lang,
  title,
  className,
}: {
  code: string;
  lang: Lang;
  title?: string;
  className?: string;
}) {
  const lines = tokenize(code, lang);
  return (
    <TerminalFrame title={title} className={className}>
      <pre className="m-0 overflow-x-auto px-4 py-3.5 font-mono text-[0.82rem] leading-relaxed">
        <code>
          {lines.map((tokens, i) => (
            <span key={i} className="block min-h-[1lh]">
              <Tokens tokens={tokens} />
            </span>
          ))}
        </code>
      </pre>
    </TerminalFrame>
  );
}
