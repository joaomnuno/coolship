import Link from "next/link";
import { SearchTrigger } from "fumadocs-ui/layouts/shared/slots/search-trigger";
import { ThemeSwitch } from "fumadocs-ui/layouts/shared/slots/theme-switch";
import { Menu } from "lucide-react";
import { Logo } from "@/components/logo";
import { repo, siteName } from "@/lib/site";
import { buttonClass } from "./button";

const links = [
  { label: "Docs", href: "/docs" },
  { label: "Guides", href: "/docs/guides/ci" },
  { label: "GitHub", href: repo.url, external: true },
];

function GitHubIcon(props: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true" {...props}>
      <path d="M12 .5C5.65.5.5 5.65.5 12c0 5.08 3.29 9.39 7.86 10.91.58.1.79-.25.79-.56v-2.17c-3.2.7-3.87-1.36-3.87-1.36-.52-1.33-1.28-1.68-1.28-1.68-1.04-.71.08-.7.08-.7 1.15.08 1.76 1.19 1.76 1.19 1.03 1.76 2.7 1.25 3.35.96.1-.75.4-1.25.73-1.54-2.55-.29-5.24-1.28-5.24-5.69 0-1.26.45-2.29 1.19-3.09-.12-.29-.52-1.46.11-3.05 0 0 .97-.31 3.17 1.18a11 11 0 0 1 5.78 0c2.2-1.49 3.17-1.18 3.17-1.18.63 1.59.23 2.76.11 3.05.74.8 1.19 1.83 1.19 3.09 0 4.42-2.69 5.39-5.26 5.68.41.36.78 1.06.78 2.14v3.17c0 .31.21.67.8.56A11.5 11.5 0 0 0 23.5 12C23.5 5.65 18.35.5 12 .5z" />
    </svg>
  );
}

/**
 * The landing page's sticky header. Search and the theme switch are the
 * same components the docs layout uses, so the two halves of the site behave
 * alike. The mobile menu is a <details>, keyboard-operable without script.
 */
export function SiteNav() {
  return (
    <header className="sticky top-0 z-50 border-b border-fd-border bg-fd-background/75 backdrop-blur-md">
      <a
        href="#main"
        className="sr-only focus:not-sr-only focus:absolute focus:top-2 focus:left-2 focus:z-50 focus:rounded-md focus:bg-fd-primary focus:px-3 focus:py-2 focus:text-sm focus:text-fd-primary-foreground"
      >
        Skip to content
      </a>
      <div className="mx-auto flex h-16 max-w-6xl items-center gap-6 px-4 sm:px-6">
        <Link
          href="/"
          className="inline-flex items-center gap-2 rounded-md focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-fd-primary"
        >
          <Logo className="size-6" />
          <span className="text-[1.05rem] font-semibold tracking-tight">
            {siteName}
          </span>
        </Link>
        <nav aria-label="Primary" className="hidden items-center gap-1 md:flex">
          {links.map((link) =>
            link.external ? (
              <a
                key={link.href}
                href={link.href}
                target="_blank"
                rel="noreferrer noopener"
                className={buttonClass("ghost", "sm", "gap-1.5")}
              >
                <GitHubIcon className="size-4" />
                {link.label}
              </a>
            ) : (
              <Link
                key={link.href}
                href={link.href}
                className={buttonClass("ghost", "sm")}
              >
                {link.label}
              </Link>
            ),
          )}
        </nav>
        <div className="ml-auto flex items-center gap-2">
          <SearchTrigger hideIfDisabled className="hidden sm:inline-flex" />
          <ThemeSwitch className="hidden sm:inline-flex" />
          <a href="/#install" className={buttonClass("primary", "sm")}>
            Install
          </a>
          <details className="group relative md:hidden">
            <summary
              className={buttonClass(
                "secondary",
                "icon",
                "list-none [&::-webkit-details-marker]:hidden",
              )}
              aria-label="Menu"
            >
              <Menu />
            </summary>
            <div className="absolute right-0 mt-2 flex w-56 flex-col gap-1 rounded-xl border border-fd-border bg-fd-popover p-2 shadow-xl">
              {links.map((link) =>
                link.external ? (
                  <a
                    key={link.href}
                    href={link.href}
                    target="_blank"
                    rel="noreferrer noopener"
                    className={buttonClass(
                      "ghost",
                      "sm",
                      "justify-start gap-2",
                    )}
                  >
                    <GitHubIcon className="size-4" />
                    {link.label}
                  </a>
                ) : (
                  <Link
                    key={link.href}
                    href={link.href}
                    className={buttonClass("ghost", "sm", "justify-start")}
                  >
                    {link.label}
                  </Link>
                ),
              )}
              <div className="mt-1 flex flex-col gap-2 border-t border-fd-border px-2 pt-2 sm:hidden">
                <div className="flex items-center justify-between">
                  <span className="text-xs text-fd-muted-foreground">
                    Search
                  </span>
                  <SearchTrigger hideIfDisabled />
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-xs text-fd-muted-foreground">
                    Theme
                  </span>
                  <ThemeSwitch />
                </div>
              </div>
            </div>
          </details>
        </div>
      </div>
    </header>
  );
}
