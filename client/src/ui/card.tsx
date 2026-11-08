import type { HTMLAttributes, ReactNode } from 'react';
import { cn } from './cn';

export function Card({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('panel', className)} {...props} />;
}

export function CardHeader({ title, description, action }: { title: ReactNode; description?: ReactNode; action?: ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-[var(--u-5)] border-b border-border px-[var(--pad-card)] py-[var(--u-6)]">
      <div className="min-w-0">
        <h2 className="text-phi-xl font-semibold tracking-tight">{title}</h2>
        {description ? <p className="mt-[var(--u-1)] text-phi-sm text-muted-foreground">{description}</p> : null}
      </div>
      {action}
    </div>
  );
}

export function CardBody({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('px-[var(--pad-card)] py-[var(--u-6)]', className)} {...props} />;
}
