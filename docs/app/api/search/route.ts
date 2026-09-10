import { createFromSource } from 'fumadocs-core/search/server';
import { source } from '@/lib/source';

// Orama search on the server: the index is built from the content when the
// server starts, and the default search dialog queries this route.
export const { GET } = createFromSource(source, {
  language: 'english',
});
