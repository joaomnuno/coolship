import type { BaseLayoutProps } from 'fumadocs-ui/layouts/shared';
import { Logo } from '@/components/logo';
import { repo, siteName } from './site';

export function baseOptions(): BaseLayoutProps {
  return {
    nav: {
      title: (
        <span className="inline-flex items-center gap-2">
          <Logo className="size-5" />
          <span className="font-semibold tracking-tight">{siteName}</span>
        </span>
      ),
    },
    links: [
      { text: 'Docs', url: '/docs', active: 'nested-url' },
      { text: 'Changelog', url: `${repo.url}/blob/${repo.branch}/CHANGELOG.md`, external: true },
      { text: 'Releases', url: `${repo.url}/releases`, external: true },
    ],
    githubUrl: repo.url,
  };
}
