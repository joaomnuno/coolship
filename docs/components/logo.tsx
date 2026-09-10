import type { ComponentProps } from 'react';

/** The Coolship mark: a prompt in a rounded square, in the current primary color. */
export function Logo(props: ComponentProps<'svg'>) {
  return (
    <svg viewBox="0 0 32 32" aria-hidden="true" {...props}>
      <rect width="32" height="32" rx="6" className="fill-fd-primary" />
      <text
        x="16"
        y="22"
        fontFamily="ui-monospace, Menlo, Consolas, monospace"
        fontSize="17"
        fontWeight="700"
        textAnchor="middle"
        className="fill-fd-primary-foreground"
      >
        &gt;_
      </text>
    </svg>
  );
}
