import type { ComponentProps, ReactNode } from "react";
import { cn } from "./cn";

/** A landmark section with the page's horizontal rhythm and vertical spacing. */
export function Section({
  className,
  children,
  ...props
}: ComponentProps<"section">) {
  return (
    <section
      className={cn(
        "mx-auto w-full max-w-6xl px-4 py-20 sm:px-6 sm:py-28",
        className,
      )}
      {...props}
    >
      {children}
    </section>
  );
}

export function Heading({
  id,
  children,
  className,
}: {
  id?: string;
  children: ReactNode;
  className?: string;
}) {
  return (
    <h2
      id={id}
      className={cn(
        "mt-3 text-3xl font-bold tracking-tight text-balance sm:text-4xl",
        className,
      )}
    >
      {children}
    </h2>
  );
}

export function Lede({
  children,
  className,
}: {
  children: ReactNode;
  className?: string;
}) {
  return (
    <p
      className={cn(
        "mt-4 max-w-2xl text-lg text-pretty text-fd-muted-foreground",
        className,
      )}
    >
      {children}
    </p>
  );
}
