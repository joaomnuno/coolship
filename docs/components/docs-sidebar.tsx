'use client';

import type * as PageTree from 'fumadocs-core/page-tree';

/**
 * A section heading in the documentation sidebar, from a separator in
 * `content/docs/meta.json` (`---Concepts---`). Fumadocs draws a separator with
 * the same weight as a page link; these are the small uppercase labels that
 * make the sections read as areas of the product rather than as folders, with
 * more space above each one than below it so the group below it holds together.
 */
export function SidebarSection({ item }: { item: PageTree.Separator }) {
  return (
    <p className="mt-7 mb-1.5 inline-flex items-center gap-2 px-2 text-[0.6875rem] font-semibold tracking-[0.045em] text-fd-muted-foreground uppercase first:mt-1 [&_svg]:size-3.5">
      {item.icon}
      {item.name}
    </p>
  );
}
