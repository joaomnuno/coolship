import Link from 'next/link';
import { HomeLayout } from 'fumadocs-ui/layouts/home';
import { baseOptions } from '@/lib/layout.shared';

export default function NotFound() {
  return (
    <HomeLayout {...baseOptions()}>
      <main className="flex flex-1 flex-col items-center justify-center gap-4 px-4 py-24 text-center">
        <p className="font-mono text-sm text-fd-muted-foreground">404</p>
        <h1 className="text-2xl font-semibold tracking-tight">Page not found</h1>
        <p className="max-w-md text-fd-muted-foreground">
          The page may have moved. Start from the documentation index, or search with{' '}
          <kbd className="rounded border px-1 font-mono text-xs">⌘K</kbd>.
        </p>
        <Link href="/docs" className="font-medium text-fd-primary underline underline-offset-4">
          Documentation
        </Link>
      </main>
    </HomeLayout>
  );
}
