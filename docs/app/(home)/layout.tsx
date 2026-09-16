// The home page draws its own navigation bar instead of this layout. vinext
// renders a 404 for an unknown top-level URL inside this route group's layout,
// while `next build` renders it inside the root layout only, so a bar here
// plus the one in app/not-found.tsx showed twice under vinext.
export default function Layout({ children }: LayoutProps<"/">) {
  return children;
}
