import { repo, siteUrl } from "../site.config.mjs";

export { repo, siteUrl };

export const siteName = "Coolship";

export const siteDescription =
  "A project-local developer CLI for Coolify: link a repository to its Coolify application once, then deploy, read logs, and sync variables from the terminal.";

export const installCommand =
  "curl -fsSL https://raw.githubusercontent.com/joaomnuno/coolship/main/scripts/install.sh | sh";

/** The install script's source on GitHub, linked from the install block. */
export const installScriptUrl = `${repo.url}/blob/${repo.branch}/scripts/install.sh`;

/**
 * The newest full release, shown on the install buttons. The site builds
 * without network access (both Dockerfiles), so this is not read from the
 * GitHub API; bump it when a release is tagged.
 */
export const latestVersion = "v0.3.0";

/** The Coolify version every command was verified against (README, Server compatibility). */
export const verifiedCoolifyVersion = "4.3.18";

/**
 * The walkthrough recording shown under the hero. While it is empty the frame
 * shows the poster with a "recording coming soon" state; set it to a YouTube
 * or Vimeo page or embed URL, or a direct .mp4/.webm URL, and the frame
 * becomes a player that loads only after the viewer presses play.
 */
export const showcaseVideoUrl = "";

/** Poster of that frame: a rendering of a real `coolship deploy` transcript. */
export const showcasePoster = "/showcase-poster.svg";

/** Absolute public URL of a site-relative path. */
export function absoluteUrl(path: string) {
  return siteUrl + path;
}
