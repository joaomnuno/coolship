import { SiteNav } from "@/components/landing/nav";

export default function Layout({ children }: LayoutProps<"/">) {
  return (
    <>
      <SiteNav />
      {children}
    </>
  );
}
