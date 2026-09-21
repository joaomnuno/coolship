import { createElement } from 'react';
import type { LLMsOptions } from 'fumadocs-core/mdx-plugins/remark-llms';
import { loader } from 'fumadocs-core/source';
import { metaSchema, pageSchema } from 'fumadocs-core/source/schema';
import { defineDocs } from 'fumadocs-mdx/macro';
import type { LucideIcon } from 'lucide-react';
import {
  Binary,
  BookOpen,
  Bot,
  Boxes,
  CirclePlay,
  CloudUpload,
  FlaskConical,
  Gauge,
  KeyRound,
  Link2,
  LockKeyhole,
  Rocket,
  Server,
  Terminal,
  TriangleAlert,
  Workflow,
} from 'lucide-react';

/**
 * The icons a page may name in its `icon` frontmatter, drawn next to its title
 * in the sidebar. Fumadocs bundles no icon library, so the loader resolves the
 * name itself and only the icons named here are bundled; an unknown name fails
 * the build rather than rendering nothing. One icon per page, monochrome: the
 * sidebar colours them, and a page with no icon (every command page under
 * Command reference) sits flush with its siblings.
 */
const icons = {
  Binary,
  BookOpen,
  Bot,
  Boxes,
  CirclePlay,
  CloudUpload,
  FlaskConical,
  Gauge,
  KeyRound,
  Link2,
  LockKeyhole,
  Rocket,
  Server,
  Terminal,
  TriangleAlert,
  Workflow,
} satisfies Record<string, LucideIcon>;

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
  icon(name) {
    if (!name) return;
    const Icon = icons[name as keyof typeof icons];
    if (!Icon) throw new Error(`Unknown icon "${name}": add it to the icons map in lib/source.ts.`);
    // The sidebar sizes and colours it; 1.8 keeps the stroke from shouting at 16px.
    return createElement(Icon, { strokeWidth: 1.8, 'aria-hidden': true });
  },
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
