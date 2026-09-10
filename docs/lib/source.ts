import type { LLMsOptions } from 'fumadocs-core/mdx-plugins/remark-llms';
import { loader } from 'fumadocs-core/source';
import { metaSchema, pageSchema } from 'fumadocs-core/source/schema';
import { defineDocs } from 'fumadocs-mdx/macro';

/**
 * The components that lib/llm.ts renders into plain Markdown for the per-page
 * Markdown route and llms-full.txt. Any other component is stringified as JSX.
 */
const markdownComponents = ['Callout', 'Cards', 'Card', 'Steps', 'Step', 'Tabs', 'Tab'];

/**
 * Stringifies one of those components as the placeholder that
 * `renderPlaceholder` in lib/llm.ts resolves per request: the same shape as
 * fumadocs-core's `placeholder()` (its `mdxAsPlaceholder` option), except that
 * a block component's children are serialized as flow content. `placeholder()`
 * serializes every element with `containerPhrasing`, which drops the blank
 * lines between the headings, paragraphs, and code fences inside a <Step> or
 * a <Card>, so `## Install` was glued to the paragraph after it and a closing
 * fence to the line that follows.
 */
const stringifyComponent: LLMsOptions['stringify'] = (node, _parent, state, info) => {
  if (node.type !== 'mdxJsxFlowElement' && node.type !== 'mdxJsxTextElement') return;
  if (!node.name || !markdownComponents.includes(node.name)) return;
  const attributes: Record<string, unknown> = {};
  for (const attribute of node.attributes) {
    if (attribute.type === 'mdxJsxExpressionAttribute') continue;
    attributes[attribute.name] = attribute.value;
  }
  const children =
    node.type === 'mdxJsxFlowElement' ? state.containerFlow(node, info) : state.containerPhrasing(node, info);
  return `\0${JSON.stringify({ name: node.name, children, attributes })}\0`;
};

const docs = defineDocs({
  dir: 'content/docs',
  docs: {
    schema: pageSchema,
    postprocess: {
      // Keep a Markdown rendition of every page for llms-full.txt and the
      // per-page Markdown route. The components above are stringified as
      // placeholders, which lib/llm.ts renders into plain Markdown.
      includeProcessedMarkdown: {
        headingIds: false,
        stringify: stringifyComponent,
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
