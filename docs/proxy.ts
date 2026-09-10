import { isMarkdownPreferred } from 'fumadocs-core/negotiation';
import { NextResponse, type NextRequest } from 'next/server';
import { markdownRoutePrefix } from '@/lib/source';

// Serves every docs page as Markdown, two ways:
//   /docs/commands/deploy.md   (and /docs/index.md for the docs index)
//   /docs/commands/deploy      with a request that prefers text/markdown
// Both are rewritten to the app/llms.mdx/docs route handler, which renders
// the page's Markdown; the HTML page is untouched otherwise.
function markdownTarget(pathname: string): string | undefined {
  if (pathname === '/docs/index.md') return `${markdownRoutePrefix}/content.md`;
  const suffix = /^\/docs\/(.+)\.md$/.exec(pathname);
  if (suffix) return `${markdownRoutePrefix}/${suffix[1]}/content.md`;
  return undefined;
}

function negotiatedTarget(pathname: string): string | undefined {
  if (pathname === '/docs') return `${markdownRoutePrefix}/content.md`;
  const nested = /^\/docs\/(.+)$/.exec(pathname);
  if (nested && !nested[1].includes('.')) return `${markdownRoutePrefix}/${nested[1]}/content.md`;
  return undefined;
}

export default function proxy(request: NextRequest) {
  const { pathname } = request.nextUrl;

  const explicit = markdownTarget(pathname);
  if (explicit) return NextResponse.rewrite(new URL(explicit, request.nextUrl));

  if (isMarkdownPreferred(request)) {
    const negotiated = negotiatedTarget(pathname);
    if (negotiated) {
      return NextResponse.rewrite(new URL(negotiated, request.nextUrl), {
        // The page URL has two representations, selected by Accept.
        headers: { Vary: 'Accept' },
      });
    }
  }

  return NextResponse.next();
}

export const config = {
  matcher: ['/docs', '/docs/:path*'],
};
