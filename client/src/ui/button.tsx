import { forwardRef, type ButtonHTMLAttributes } from 'react';
import { cn } from './cn';

type Variant = 'primary' | 'secondary' | 'ghost' | 'danger';
type Size = 'sm' | 'md' | 'lg';

const variants: Record<Variant, string> = {
  primary: 'bg-primary text-primary-foreground hover:opacity-[0.92]',
  secondary: 'bg-muted text-foreground hover:bg-muted/80 border border-border',
  ghost: 'bg-transparent text-foreground hover:bg-muted',
  danger: 'bg-destructive text-white hover:opacity-[0.92]',
};

const sizes: Record<Size, string> = {
  sm: 'h-[var(--u-8)] px-[var(--u-4)] text-phi-sm',
  md: 'h-[var(--u-9)] px-[var(--u-6)] text-phi-base',
  lg: 'h-[var(--u-10)] px-[var(--u-7)] text-phi-lg',
};

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: Variant;
  size?: Size;
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { className, variant = 'primary', size = 'md', type = 'button', ...props },
  ref,
) {
  return (
    <button
      ref={ref}
      type={type}
      className={cn(
        'inline-flex items-center justify-center gap-[var(--u-2)] rounded-[calc(var(--radius)*0.8)] font-medium transition-colors',
        'disabled:pointer-events-none disabled:opacity-50',
        variants[variant],
        sizes[size],
        className,
      )}
      {...props}
    />
  );
});
