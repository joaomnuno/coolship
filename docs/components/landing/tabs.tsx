"use client";
import {
  useId,
  useRef,
  useState,
  type KeyboardEvent,
  type ReactNode,
} from "react";
import { cn } from "./cn";

export interface TabItem {
  value: string;
  label: string;
  content: ReactNode;
}

/**
 * A WAI-ARIA tab list: one tab stop, arrow keys move and select, Home and
 * End jump. Panels stay in the document (hidden) so their content is in the
 * server-rendered HTML.
 */
export function Tabs({
  items,
  label,
  className,
}: {
  items: TabItem[];
  label: string;
  className?: string;
}) {
  const id = useId();
  const [active, setActive] = useState(items[0]?.value);
  const tabRefs = useRef<Array<HTMLButtonElement | null>>([]);

  function focusTab(index: number) {
    const next = (index + items.length) % items.length;
    setActive(items[next].value);
    tabRefs.current[next]?.focus();
  }

  function onKeyDown(event: KeyboardEvent<HTMLButtonElement>, index: number) {
    const keys: Record<string, () => void> = {
      ArrowRight: () => focusTab(index + 1),
      ArrowLeft: () => focusTab(index - 1),
      Home: () => focusTab(0),
      End: () => focusTab(items.length - 1),
    };
    const handler = keys[event.key];
    if (handler) {
      event.preventDefault();
      handler();
    }
  }

  return (
    <div className={className}>
      <div
        role="tablist"
        aria-label={label}
        className="-mx-4 flex gap-1 overflow-x-auto px-4 pb-px sm:mx-0 sm:px-0 [scrollbar-width:none]"
      >
        {items.map((item, index) => {
          const selected = item.value === active;
          return (
            <button
              key={item.value}
              ref={(el) => {
                tabRefs.current[index] = el;
              }}
              type="button"
              role="tab"
              id={`${id}-tab-${item.value}`}
              aria-selected={selected}
              aria-controls={`${id}-panel-${item.value}`}
              tabIndex={selected ? 0 : -1}
              onClick={() => setActive(item.value)}
              onKeyDown={(event) => onKeyDown(event, index)}
              className={cn(
                "relative shrink-0 rounded-md px-3.5 py-2 font-mono text-sm transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-fd-primary",
                selected
                  ? "bg-fd-primary/12 text-fd-primary"
                  : "text-fd-muted-foreground hover:bg-fd-accent hover:text-fd-foreground",
              )}
            >
              {item.label}
            </button>
          );
        })}
      </div>
      {items.map((item) => (
        <div
          key={item.value}
          role="tabpanel"
          id={`${id}-panel-${item.value}`}
          aria-labelledby={`${id}-tab-${item.value}`}
          hidden={item.value !== active}
          tabIndex={0}
          className="mt-5 rounded-xl focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-fd-primary"
        >
          {item.content}
        </div>
      ))}
    </div>
  );
}
