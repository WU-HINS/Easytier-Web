import { Navigate, Outlet } from 'react-router-dom';
import { useConsoleInfo } from '../features/auth/use-auth';

/**
 * The single place that decides "is this an administrator".
 *
 * The role comes from the server on every mount, never from a cached claim and
 * never from a second copy of the rule: one predicate means a route cannot drift
 * out of step with the navigation that hides it.
 */
export function RequireAdmin() {
  const info = useConsoleInfo();
  if (info.isLoading) return null;
  if (info.data?.role !== 'admin') return <Navigate to="/" replace />;
  return <Outlet />;
}
