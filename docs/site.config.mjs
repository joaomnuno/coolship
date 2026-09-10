// Shared by lib/site.ts (pages and routes) and anything that runs outside
// the app, so the public URL and the repository are defined once.

/**
 * Public origin of the site, without a trailing slash. Used for the absolute
 * URLs in llms.txt, the per-page Markdown, canonical links, and the prompts
 * handed to assistants. Override with SITE_URL when the site is served
 * elsewhere (a preview deployment, a local container).
 */
export const siteUrl = (process.env.SITE_URL || 'https://coolship.itrocas.com').replace(/\/+$/, '');

export const repo = {
  owner: 'joaomnuno',
  name: 'coolship',
  branch: 'main',
  url: 'https://github.com/joaomnuno/coolship',
};
