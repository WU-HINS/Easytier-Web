import { forwardRef, type InputHTMLAttributes } from 'react';
import { cn } from './cn';

export const Input = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement>>(
  function Input({ className, ...props }, ref) {
    return (
      <input
        ref={ref}
        className={cn(
          'h-[var(--u-10)] w-full rounded-[calc(var(--radius)*0.8)] border border-input bg-card px-[var(--u-5)]',
          'text-phi-base placeholder:text-muted-foreground focus-visible:border-ring',
          className,
        )}
        {...props}
      />
    );
  },
);
