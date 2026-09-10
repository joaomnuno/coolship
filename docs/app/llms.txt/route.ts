import { getLLMIndex } from '@/lib/llm';

export const revalidate = false;

export function GET() {
  return new Response(getLLMIndex(), {
    headers: { 'Content-Type': 'text/markdown; charset=utf-8' },
  });
}
