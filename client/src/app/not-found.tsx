import { Link, useLocation } from 'react-router-dom';
import { useCopy } from '../copy/locale';

/**
 * An address that does not exist has to say so.
 *
 * The catch-all route used to redirect straight to the overview, so a mistyped URL or
 * a stale bookmark answered "everything is fine" and rewrote the address bar. The
 * operator then believes they misremembered, and a real 404 is never reported
 * (review round r259, R-C).
 */
export function NotFoundPage() {
  const copy = useCopy();
  const location = useLocation();
  return (
    <div className="flex min-h-[50vh] items-center justify-center p-[var(--gap-section)]">
      <div className="panel w-full max-w-[var(--w-narrow)] p-[var(--pad-card)] text-center">
        <p className="text-phi-2xl font-semibold tracking-tight">404</p>
        <h1 className="mt-[var(--u-2)] text-phi-xl font-semibold">{copy.notFound.title}</h1>
        <p className="mt-[var(--u-3)] text-phi-sm text-muted-foreground">
          {copy.notFound.hint}
        </p>
        <code className="mt-[var(--u-2)] block break-all rounded-[var(--radius)] bg-muted px-[var(--u-3)] py-[var(--u-2)] text-phi-sm">
          {location.pathname}
        </code>
        <Link className="mt-[var(--u-5)] inline-flex min-h-[var(--u-11)] items-center text-phi-sm text-primary underline underline-offset-4" to="/">
          {copy.notFound.back}
        </Link>
      </div>
    </div>
  );
}
