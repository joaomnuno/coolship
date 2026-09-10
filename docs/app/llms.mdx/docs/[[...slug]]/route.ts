import { notFound } from 'next/navigation';
import { getLLMText } from '@/lib/llm';
import { markdownRoute, source } from '@/lib/source';

// Renders one page as Markdown. Readers reach it through proxy.ts, which
// rewrites /docs/<page>.md and Accept: text/markdown requests here.
export const revalidate = false;

export async function GET(_req: Request, { params }: RouteContext<'/llms.mdx/docs/[[...slug]]'>) {
  const { slug } = await params;
  // Drop the trailing "content.md" segment.
  const page = source.getPage(slug?.slice(0, -1));
  if (!page) notFound();

  return new Response(await getLLMText(page), {
    headers: {
      'Content-Type': 'text/markdown; charset=utf-8',
      // A page URL answers with this or with HTML depending on Accept. proxy.ts
      // says so too, but a route handler's own headers are the ones that
      // survive prerendering on the Next.js build, so the Markdown response
      // carries the header itself.
      Vary: 'Accept',
    },
  });
}

export function generateStaticParams() {
  return source.getPages().map((page) => ({ slug: markdownRoute(page).segments }));
}
