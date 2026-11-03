import type { ReactNode } from 'react';
import { cn } from './cn';

type Tone = 'neutral' | 'success' | 'warning' | 'danger' | 'primary';

const tones: Record<Tone, string> = {
  neutral: 'bg-muted text-muted-foreground',
  success: 'bg-success/12 text-success',
  warning: 'bg-warning/15 text-warning',
  danger: 'bg-destructive/12 text-destructive',
  primary: 'bg-primary/12 text-primary',
};

export function Badge({ tone = 'neutral', children }: { tone?: Tone; children: ReactNode }) {
  return (
    <span className={cn('inline-flex items-center gap-1 rounded-full px-[var(--u-3)] py-[calc(var(--u-3)/4)] text-phi-sm font-medium', tones[tone])}>
      {children}
    </span>
  );
}
