"use client";
import { useEffect, useMemo, useRef, useState } from "react";
import { Pause, Play, RotateCcw } from "lucide-react";
import { buttonClass } from "./button";
import { cn } from "./cn";
import { TerminalFrame, Tokens } from "./code";
import { tokenizeLine } from "./highlight";

export interface ReplayLine {
  text: string;
  /** `dim` for the server's build log, which deploy relays on stderr. */
  kind?: "dim" | "out";
  /** Milliseconds before the line appears; defaults to a quick 90. */
  delay?: number;
}

export interface ReplaySegment {
  command: string;
  output: ReplayLine[];
}

interface Rendered {
  text: string;
  kind: "typing" | "cmd" | "out" | "dim" | "blank";
}

interface Frame {
  lines: Rendered[];
  /** Milliseconds to wait before showing this frame. */
  delay: number;
}

const typingMs = 32;

/** Expands the script into frames: one per typed character and per output line. */
function buildFrames(script: ReplaySegment[]): Frame[] {
  const frames: Frame[] = [];
  const lines: Rendered[] = [];
  const push = (delay: number) => frames.push({ lines: [...lines], delay });

  script.forEach((segment, index) => {
    if (index > 0) lines.push({ text: "", kind: "blank" });
    lines.push({ text: "", kind: "typing" });
    push(index === 0 ? 500 : 900);
    for (let i = 1; i <= segment.command.length; i++) {
      lines[lines.length - 1] = {
        text: segment.command.slice(0, i),
        kind: "typing",
      };
      push(typingMs);
    }
    lines[lines.length - 1] = { text: segment.command, kind: "cmd" };
    push(400);
    for (const line of segment.output) {
      lines.push({ text: line.text, kind: line.kind ?? "out" });
      push(line.delay ?? 90);
    }
  });
  return frames;
}

function transcript(script: ReplaySegment[]) {
  return script
    .map((segment) =>
      [`$ ${segment.command}`, ...segment.output.map((line) => line.text)].join(
        "\n",
      ),
    )
    .join("\n\n");
}

function Line({ line }: { line: Rendered }) {
  switch (line.kind) {
    case "blank":
      return <span className="block min-h-[1lh]" />;
    case "typing":
    case "cmd":
      return (
        <span className="block min-h-[1lh]">
          <span className="tk-prompt">$ </span>
          <span className="tk-cmd">{line.text}</span>
          {line.kind === "typing" && (
            <span className="caret" aria-hidden="true" />
          )}
        </span>
      );
    case "dim":
      return <span className="tk-dim block min-h-[1lh]">{line.text}</span>;
    default:
      return (
        <span className="block min-h-[1lh]">
          <Tokens tokens={tokenizeLine(line.text, "text")} />
        </span>
      );
  }
}

type Status = "static" | "idle" | "playing" | "paused" | "done";

const statusText: Record<Status, string> = {
  static:
    "Showing the final output; animation is off because your system prefers reduced motion.",
  idle: "Ready to play.",
  playing: "Playing.",
  paused: "Paused.",
  done: "Finished.",
};

/**
 * Replays a real session with typewriter timing. The server renders the final
 * frame, so the transcript is complete without JavaScript and for readers who
 * prefer reduced motion; otherwise the replay rewinds on mount and starts when
 * it scrolls into view. The full transcript is also available to assistive
 * technology as plain text, and the animated copy is hidden from it.
 */
export function TerminalReplay({
  script,
  title,
}: {
  script: ReplaySegment[];
  title: string;
}) {
  const frames = useMemo(() => buildFrames(script), [script]);
  const last = frames.length - 1;
  const [frame, setFrame] = useState(last);
  const [status, setStatus] = useState<Status>("static");
  const root = useRef<HTMLDivElement>(null);
  const scroller = useRef<HTMLPreElement>(null);
  const started = useRef(false);

  useEffect(() => {
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    setFrame(0);
    setStatus("idle");
    const element = root.current;
    if (!element) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (started.current || !entries.some((entry) => entry.isIntersecting))
          return;
        started.current = true;
        setStatus("playing");
        observer.disconnect();
      },
      { threshold: 0.4 },
    );
    observer.observe(element);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    if (status !== "playing") return;
    if (frame >= last) {
      setStatus("done");
      return;
    }
    const timer = window.setTimeout(
      () => setFrame((current) => current + 1),
      frames[frame + 1].delay,
    );
    return () => window.clearTimeout(timer);
  }, [status, frame, frames, last]);

  useEffect(() => {
    const element = scroller.current;
    if (element) element.scrollTop = element.scrollHeight;
  }, [frame]);

  function toggle() {
    started.current = true;
    if (status === "playing") setStatus("paused");
    else if (status === "done") replay();
    else setStatus("playing");
  }

  function replay() {
    started.current = true;
    setFrame(0);
    setStatus("playing");
  }

  const interactive = status !== "static";
  const current = frames[frame];

  return (
    <div ref={root}>
      <TerminalFrame
        title={title}
        aside={
          interactive && (
            <>
              <button
                type="button"
                onClick={toggle}
                className={buttonClass(
                  "ghost",
                  "sm",
                  "h-7 gap-1.5 px-2 text-xs text-fd-muted-foreground hover:text-white",
                )}
                aria-label={
                  status === "playing"
                    ? "Pause the replay"
                    : status === "done"
                      ? "Replay"
                      : "Play the replay"
                }
              >
                {status === "playing" ? (
                  <Pause />
                ) : status === "done" ? (
                  <RotateCcw />
                ) : (
                  <Play />
                )}
                {status === "playing"
                  ? "Pause"
                  : status === "done"
                    ? "Replay"
                    : "Play"}
              </button>
              {status !== "done" && status !== "idle" && (
                <button
                  type="button"
                  onClick={replay}
                  className={buttonClass(
                    "ghost",
                    "sm",
                    "h-7 gap-1.5 px-2 text-xs text-fd-muted-foreground hover:text-white",
                  )}
                  aria-label="Restart the replay"
                >
                  <RotateCcw />
                  Restart
                </button>
              )}
            </>
          )
        }
      >
        <pre
          ref={scroller}
          aria-hidden="true"
          className="m-0 h-[22rem] overflow-auto px-4 py-4 font-mono text-[0.8rem] leading-relaxed sm:h-[26rem] sm:text-[0.84rem]"
        >
          <code>
            {current.lines.map((line, i) => (
              <Line key={i} line={line} />
            ))}
          </code>
        </pre>
        <pre className="sr-only">{transcript(script)}</pre>
        <div className="term-bar h-1" aria-hidden="true">
          <div
            className={cn(
              "h-full bg-fd-primary transition-[width] duration-150",
              !interactive && "hidden",
            )}
            style={{ width: `${(frame / last) * 100}%` }}
          />
        </div>
      </TerminalFrame>
      <p role="status" className="sr-only">
        {statusText[status]}
      </p>
    </div>
  );
}
