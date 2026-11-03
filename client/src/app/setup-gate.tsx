import { useQuery } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { Navigate } from 'react-router-dom';
import { api } from '../lib/http';

/**
 * Blocks the whole application until the first-run wizard has been completed:
 * an uninitialized console has no accounts at all.
 */
export function SetupGate({ children }: { children: ReactNode }) {
  const status = useQuery({ queryKey: ['setup-status'], queryFn: api.setupStatus, staleTime: 5_000 });
  if (status.isLoading) return null;
  if (status.data && !status.data.initialized) return <Navigate to="/setup" replace />;
  return <>{children}</>;
}
