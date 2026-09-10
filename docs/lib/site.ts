import { repo, siteUrl } from '../site.config.mjs';

export { repo, siteUrl };

export const siteName = 'Coolship';

export const siteDescription =
  'A project-local developer CLI for Coolify: link a repository to its Coolify application once, then deploy, read logs, and sync variables from the terminal.';

export const installCommand =
  'curl -fsSL https://raw.githubusercontent.com/joaomnuno/coolship/main/scripts/install.sh | sh';

/** Absolute public URL of a site-relative path. */
export function absoluteUrl(path: string) {
  return siteUrl + path;
}
