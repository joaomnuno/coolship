import { getLLMText, orderedPages } from '@/lib/llm';

export const revalidate = false;

export async function GET() {
  const pages = await Promise.all(orderedPages().map(getLLMText));

  return new Response(pages.join('\n---\n\n'), {
    headers: { 'Content-Type': 'text/markdown; charset=utf-8' },
  });
}
