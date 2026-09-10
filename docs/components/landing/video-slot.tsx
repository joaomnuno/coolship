"use client";
import { useMemo, useState } from "react";
import { Play, Video } from "lucide-react";

type Embed =
  | { kind: "none" }
  | { kind: "iframe"; src: string }
  | { kind: "file"; src: string };

function youtube(id: string): Embed {
  return {
    kind: "iframe",
    src: `https://www.youtube-nocookie.com/embed/${id}?autoplay=1&rel=0`,
  };
}

/** Turns a YouTube or Vimeo page URL, an embed URL, or a media file URL into an embed. */
export function resolveEmbed(url: string): Embed {
  if (!url) return { kind: "none" };
  let parsed: URL;
  try {
    parsed = new URL(url);
  } catch {
    return { kind: "file", src: url };
  }
  const host = parsed.hostname.replace(/^(www|m)\./, "");
  if (host === "youtu.be") {
    const id = parsed.pathname.split("/")[1];
    if (id) return youtube(id);
  }
  if (host === "youtube.com" || host === "youtube-nocookie.com") {
    const id =
      parsed.searchParams.get("v") ??
      /\/(?:embed|shorts|v)\/([\w-]+)/.exec(parsed.pathname)?.[1];
    if (id) return youtube(id);
  }
  if (host === "vimeo.com" || host === "player.vimeo.com") {
    const id = /\/(?:video\/)?(\d+)/.exec(parsed.pathname)?.[1];
    if (id)
      return {
        kind: "iframe",
        src: `https://player.vimeo.com/video/${id}?autoplay=1&dnt=1`,
      };
  }
  return { kind: "file", src: url };
}

const posterAlt =
  "A terminal running coolship deploy: the deployment is queued, the build log streams while the image builds, the rolling update reports a healthy check, and the deployment finishes.";

/**
 * The 16:9 media frame under the hero. Without a recording it shows the
 * poster and says so. With one, it shows the poster with a play button and
 * loads the player only after that button is pressed, so nothing third-party
 * loads with the page and nothing plays on its own.
 */
export function VideoSlot({
  src,
  poster,
  title,
}: {
  src: string;
  poster: string;
  title: string;
}) {
  const embed = useMemo(() => resolveEmbed(src), [src]);
  const [active, setActive] = useState(false);

  return (
    <div className="relative aspect-video w-full max-w-full overflow-hidden rounded-2xl border border-fd-border bg-[hsl(24_10%_5%)] shadow-[0_40px_120px_-40px_var(--ember-glow)]">
      {active && embed.kind === "iframe" ? (
        <iframe
          src={embed.src}
          title={title}
          className="absolute inset-0 size-full"
          allow="autoplay; fullscreen; picture-in-picture"
          allowFullScreen
          referrerPolicy="strict-origin-when-cross-origin"
        />
      ) : active && embed.kind === "file" ? (
        // eslint-disable-next-line jsx-a11y/media-has-caption -- the recording does not exist yet
        <video
          src={embed.src}
          poster={poster}
          controls
          autoPlay
          playsInline
          className="absolute inset-0 size-full"
        />
      ) : (
        <>
          <img
            src={poster}
            alt={posterAlt}
            width={1280}
            height={720}
            className="absolute inset-0 size-full object-cover"
          />
          {embed.kind === "none" ? (
            <div className="absolute inset-x-0 bottom-0 flex flex-wrap items-end justify-between gap-3 bg-gradient-to-t from-black/85 via-black/40 to-transparent p-4 pt-16 sm:p-6">
              <div className="max-w-md">
                <span className="inline-flex items-center gap-1.5 rounded-full border border-white/15 bg-black/50 px-3 py-1 font-mono text-xs text-white/90 backdrop-blur">
                  <Video
                    className="size-3.5 text-fd-primary"
                    aria-hidden="true"
                  />
                  Recording coming soon
                </span>
                <p className="mt-2 text-sm text-white/75">
                  A short walkthrough of link, deploy, and logs is being
                  recorded. Until it lands, the replay below runs the real
                  transcript.
                </p>
              </div>
            </div>
          ) : (
            <button
              type="button"
              onClick={() => setActive(true)}
              aria-label={`Play: ${title}`}
              className="group absolute inset-0 grid place-items-center focus-visible:outline-2 focus-visible:-outline-offset-4 focus-visible:outline-fd-primary"
            >
              <span className="grid size-20 place-items-center rounded-full bg-fd-primary text-fd-primary-foreground shadow-[0_0_0_10px_var(--ember-soft)] transition-transform group-hover:scale-105">
                <Play
                  className="size-8 translate-x-0.5"
                  fill="currentColor"
                  aria-hidden="true"
                />
              </span>
            </button>
          )}
        </>
      )}
    </div>
  );
}
