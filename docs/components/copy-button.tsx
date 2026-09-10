'use client';
import { useCopyButton } from 'fumadocs-ui/utils/use-copy-button';
import { Check, Copy } from 'lucide-react';

/** Copies a fixed string, such as the install command, to the clipboard. */
export function CopyButton({ text, label = 'Copy', className }: { text: string; label?: string; className?: string }) {
  const [checked, onClick] = useCopyButton(() => navigator.clipboard.writeText(text));

  return (
    <button type="button" onClick={onClick} aria-label={label} className={className}>
      {checked ? <Check className="size-4" /> : <Copy className="size-4" />}
      <span>{checked ? 'Copied' : label}</span>
    </button>
  );
}
