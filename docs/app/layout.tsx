import type { Metadata } from 'next';
import { Provider } from '@/components/provider';
import { siteDescription, siteName, siteUrl } from '@/lib/site';
import './global.css';

export const metadata: Metadata = {
  metadataBase: new URL(siteUrl),
  title: {
    default: `${siteName} — a project-local CLI for Coolify`,
    template: `%s | ${siteName}`,
  },
  description: siteDescription,
};

export default function Layout({ children }: LayoutProps<'/'>) {
  return (
    <html lang="en" suppressHydrationWarning>
      <body className="flex min-h-screen flex-col">
        <Provider>{children}</Provider>
      </body>
    </html>
  );
}
