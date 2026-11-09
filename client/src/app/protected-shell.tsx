import { Navigate, Outlet, useLocation } from 'react-router-dom';
import { ApiError } from '../lib/http';
import { Skeleton } from '../ui';
import { useCopy } from '../copy/locale';
import { describeError } from '../lib/errors';
import { useConsoleInfo } from '../features/auth/use-auth';

/**
 * Session guard. It asks the API who we are instead of trusting local state,
 * so a revoked session lands on the login page on the next navigation.
 */
export function ProtectedShell() {
  const copy = useCopy();
  const location = useLocation();
  const query = useConsoleInfo();

  if (query.isLoading) {
    return (
      <div className="flex min-h-screen items-center justify-center p-[var(--gap-section)]">
        <div className="w-full max-w-[var(--w-narrow)] space-y-[var(--gap-card)]">
          <Skeleton className="h-[var(--u-11)]" />
          <Skeleton className="h-[var(--u-12)]" />
        </div>
      </div>
    );
  }

  if (query.isError) {
    if (query.error instanceof ApiError && query.error.status === 401) {
      return <Navigate to="/login" replace state={{ from: location.pathname }} />;
    }
    return (
      <div className="flex min-h-screen items-center justify-center">
        <div className="panel p-[var(--gap-section)] text-center">
          <p className="text-phi-lg font-medium">{copy.shell.disconnected}</p>
          <p className="mt-[var(--u-3)] text-phi-sm text-muted-foreground">{describeError(query.error, copy)}</p>
        </div>
      </div>
    );
  }

  return <Outlet />;
}
