import { clsx, type ClassValue } from 'clsx';
import { extendTailwindMerge } from 'tailwind-merge';

/**
 * tailwind-merge groups utilities by name prefix. The type scale here is the
 * text-phi-* family (see tokens.css), which the default configuration reads as a
 * text *colour*, so "text-primary-foreground ... text-phi-base" lost the colour and
 * the primary button rendered dark text on a dark button: 3.56:1, below AA.
 *
 * Teaching it that text-phi-* is a font size keeps the two groups apart.
 */
const merge = extendTailwindMerge({
  extend: {
    classGroups: {
      'font-size': ['text-phi-xs', 'text-phi-sm', 'text-phi-base', 'text-phi-lg', 'text-phi-xl'],
    },
  },
});

export function cn(...inputs: ClassValue[]): string {
  return merge(clsx(inputs));
}
