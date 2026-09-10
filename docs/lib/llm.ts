import { renderPlaceholder } from 'fumadocs-core/mdx-plugins/remark-llms.runtime';
import type * as PageTree from 'fumadocs-core/page-tree';
import { absoluteUrl, siteDescription, siteName, siteUrl } from './site';
import { type Page, source } from './source';

function text(value: unknown) {
  return typeof value === 'string' ? value : '';
}

function capitalize(value: string) {
  return value.charAt(0).toUpperCase() + value.slice(1);
}

function quote(value: string) {
  return value
    .trim()
    .split('\n')
    .map((line) => (line.length > 0 ? `> ${line}` : '>'))
    .join('\n');
}

/** Root-relative links become absolute so the text stands on its own. */
function absolutize(markdown: string) {
  return markdown.replace(/\]\(\/(?!\/)/g, `](${siteUrl}/`);
}

/**
 * One page as plain Markdown: the title and URL, then the processed body.
 * The components listed in lib/source.ts arrive as placeholders whose
 * children are already Markdown blocks separated by blank lines; the
 * renderers below keep those separators.
 */
export async function getLLMText(page: Page) {
  const processed = await page.data.getText('processed');
  const body = await renderPlaceholder(processed, {
    Callout: ({ attributes, children }) => {
      const label = text(attributes.title) || capitalize(text(attributes.type) || 'note');
      return `> **${label}**\n${quote(children)}`;
    },
    // One tight list: the cards arrive as `- ` items separated by blank lines.
    Cards: ({ children }) => children.trim().replace(/\n{2,}(?=- )/g, '\n'),
    Card: ({ attributes, children }) => {
      const title = text(attributes.title);
      const href = text(attributes.href);
      // A card body with several blocks stays inside its list item.
      const body = children.trim().replace(/\n/g, '\n  ');
      const link = href ? `[${title}](${href})` : title;
      return `- ${link}${body ? `: ${body}` : ''}`;
    },
    Steps: ({ children }) => children.trim(),
    Step: ({ children }) => children.trim(),
    Tabs: ({ children }) => children.trim(),
    Tab: ({ attributes, children }) => {
      const label = text(attributes.value);
      return label ? `**${label}**\n\n${children.trim()}` : children.trim();
    },
  });

  const lines = [`# ${page.data.title}`];
  if (page.data.description) lines.push('', page.data.description);
  lines.push('', `URL: ${absoluteUrl(page.url)}`, '', absolutize(body).trim(), '');
  return lines.join('\n');
}

/** Pages in sidebar order. */
export function orderedPages(): Page[] {
  const pages: Page[] = [];
  const visit = (node: PageTree.Node) => {
    if (node.type === 'page') {
      const page = source.getNodePage(node);
      if (page) pages.push(page);
    } else if (node.type === 'folder') {
      if (node.index) visit(node.index);
      for (const child of node.children) visit(child);
    }
  };
  for (const child of source.getPageTree().children) visit(child);
  return pages;
}

/**
 * llms.txt: a title, a summary, and every page grouped by sidebar section,
 * with absolute URLs. Each entry's description comes from its frontmatter.
 */
export function getLLMIndex() {
  const out: string[] = [
    `# ${siteName}`,
    '',
    `> ${siteDescription}`,
    '',
    `This index lists every page of the Coolship documentation at ${siteUrl}/docs. Append \`.md\` to a page URL (or request it with \`Accept: text/markdown\`) to read that page as Markdown, or fetch ${siteUrl}/llms-full.txt for the whole documentation in one file. Coolship is verified against Coolify 4.3.18.`,
  ];

  let section: string | undefined;
  const heading = (title: string) => {
    if (section === title) return;
    section = title;
    out.push('', `## ${title}`, '');
  };

  const item = (node: PageTree.Item) => {
    if (node.external) {
      out.push(`- [${String(node.name)}](${node.url})`);
      return;
    }
    const page = source.getNodePage(node);
    if (!page) return;
    const description = page.data.description ? `: ${page.data.description}` : '';
    out.push(`- [${page.data.title}](${absoluteUrl(page.url)})${description}`);
  };

  for (const node of source.getPageTree().children) {
    if (node.type === 'page') {
      heading('Overview');
      item(node);
    } else if (node.type === 'folder') {
      const meta = source.getNodeMeta(node);
      heading(meta?.data.title ?? String(node.name));
      if (node.index) item(node.index);
      for (const child of node.children) if (child.type === 'page') item(child);
    }
  }

  out.push('');
  return out.join('\n');
}
