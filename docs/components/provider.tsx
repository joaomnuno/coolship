'use client';
import { RootProvider } from 'fumadocs-ui/provider/next';
import type { ReactNode } from 'react';

// The default search dialog queries /api/search (app/api/search/route.ts).
export function Provider({ children }: { children: ReactNode }) {
  return <RootProvider>{children}</RootProvider>;
}
