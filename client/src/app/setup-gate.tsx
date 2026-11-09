import type { ReactNode } from 'react';
import { Navigate } from 'react-router-dom';
import { useSetupStatus } from '../features/auth/use-auth';

/**
 * Blocks the whole application until the first-run wizard has been completed:
 * an uninitialized console has no accounts at all.
 */
export function SetupGate({ children }: { children: ReactNode }) {
  const status = useSetupStatus();
  if (status.isLoading) return null;
  if (status.data && !status.data.initialized) return <Navigate to="/setup" replace />;
  return <>{children}</>;
}
