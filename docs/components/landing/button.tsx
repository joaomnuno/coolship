import { cn } from "./cn";

/**
 * Button classes shared by <a> and <button>, in the shadcn style: one base,
 * a variant, and a size. Kept as a class helper rather than a component so a
 * link, a form button, and a dialog trigger can all use it.
 */
const base =
  "inline-flex shrink-0 items-center justify-center gap-2 rounded-lg font-medium whitespace-nowrap transition-[background-color,color,box-shadow,transform] duration-200 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-fd-primary disabled:pointer-events-none disabled:opacity-50 [&_svg]:size-4 [&_svg]:shrink-0";

const variants = {
  primary:
    "bg-fd-primary text-fd-primary-foreground shadow-[0_0_0_1px_var(--ember),0_10px_30px_-12px_var(--ember-glow)] hover:brightness-110 active:translate-y-px",
  secondary:
    "border border-fd-border bg-fd-card text-fd-foreground shadow-sm hover:border-fd-primary/40 hover:bg-fd-accent active:translate-y-px",
  ghost: "text-fd-muted-foreground hover:bg-fd-accent hover:text-fd-foreground",
} as const;

const sizes = {
  sm: "h-8 px-3 text-sm",
  md: "h-10 px-4 text-sm",
  lg: "h-12 px-6 text-base",
  icon: "size-9",
} as const;

export type ButtonVariant = keyof typeof variants;
export type ButtonSize = keyof typeof sizes;

export function buttonClass(
  variant: ButtonVariant = "primary",
  size: ButtonSize = "md",
  className?: string,
) {
  return cn(base, variants[variant], sizes[size], className);
}
