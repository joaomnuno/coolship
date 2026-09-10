import { loader } from 'fumadocs-core/source';
import { metaSchema, pageSchema } from 'fumadocs-core/source/schema';
import { defineDocs } from 'fumadocs-mdx/macro';

const docs = defineDocs({
  dir: 'content/docs',
  docs: {
    schema: pageSchema,
    postprocess: {
      // Keep a Markdown rendition of every page for llms-full.txt and the
      // per-page Markdown route. The components listed here are stringified
      // as placeholders, which lib/llm.ts renders into plain Markdown.
      includeProcessedMarkdown: {
        headingIds: false,
        mdxAsPlaceholder: ['Callout', 'Cards', 'Card', 'Steps', 'Step', 'Tabs', 'Tab'],
      },
    },
  },
  meta: {
    schema: metaSchema,
  },
});

// See https://fumadocs.dev/docs/headless/source-api
export const source = loader({
  baseUrl: '/docs',
  source: docs.toFumadocsSource(),
});

export type Page = (typeof source)['$inferPage'];

/** The route handler that renders a page as Markdown (app/llms.mdx/docs). */
export const markdownRoutePrefix = '/llms.mdx/docs';

export function markdownRoute(page: Page) {
  const segments = [...page.slugs, 'content.md'];
  return { segments, path: `${markdownRoutePrefix}/${segments.join('/')}` };
}

/**
 * Public Markdown path of a page: the page URL with `.md` appended
 * (`/docs/commands/deploy.md`), or `/docs/index.md` for the docs index.
 * proxy.ts rewrites these to the route handler, and also serves the page URL
 * itself as Markdown when the request prefers `text/markdown`.
 */
export function markdownPath(page: Page) {
  return page.slugs.length === 0 ? `${page.url}/index.md` : `${page.url}.md`;
}
