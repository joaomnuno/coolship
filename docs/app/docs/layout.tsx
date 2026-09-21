import type * as PageTree from 'fumadocs-core/page-tree';
import { DocsLayout } from 'fumadocs-ui/layouts/docs';
import { SidebarSection } from '@/components/docs-sidebar';
import { baseOptions } from '@/lib/layout.shared';
import { repo } from '@/lib/site';
import { source } from '@/lib/source';

/**
 * The page tree with the two product links at the end of it. They used to sit
 * above the documentation, where they competed with the pages the reader came
 * for; as a Resources section they stay one click away and out of the way.
 * They are added here rather than in `content/docs/meta.json` so that their
 * URLs keep coming from lib/site.ts and llms.txt keeps listing pages only.
 */
function treeWithResources(): PageTree.Root {
  const tree = source.getPageTree();
  const resources: PageTree.Node[] = [
    { $id: 'resources', type: 'separator', name: 'Resources' },
    {
      $id: 'resources/changelog',
      type: 'page',
      name: 'Changelog',
      url: `${repo.url}/blob/${repo.branch}/CHANGELOG.md`,
      external: true,
    },
    { $id: 'resources/releases', type: 'page', name: 'Releases', url: `${repo.url}/releases`, external: true },
  ];
  return { ...tree, children: [...tree.children, ...resources] };
}

export default function Layout({ children }: LayoutProps<'/docs'>) {
  // The same links are the Resources section of the sidebar here, so the menu
  // items above the page tree would repeat them; the landing page keeps them.
  const { links: _links, ...options } = baseOptions();

  return (
    <DocsLayout
      tree={treeWithResources()}
      {...options}
      // Command reference is the one section with pages under it, and it opens
      // itself on any command page.
      sidebar={{ defaultOpenLevel: 0, components: { Separator: SidebarSection } }}
    >
      {children}
    </DocsLayout>
  );
}
