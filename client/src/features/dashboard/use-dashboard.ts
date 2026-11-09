import { useQuery } from '@tanstack/react-query';
import { dashboardApi } from './api';

/**
 * The overview polls while the operator is watching it and stops when they say so.
 * Query keys are owned by the feature, so an invalidation elsewhere can name them
 * without knowing where the page lives.
 */
export function useSummary(interval: number | false) {
  return useQuery({ queryKey: ['summary'], queryFn: dashboardApi.summary, refetchInterval: interval });
}

export function useMachines(interval: number | false) {
  return useQuery({ queryKey: ['machines'], queryFn: dashboardApi.machines, refetchInterval: interval });
}

export function useNetworks(interval: number | false) {
  return useQuery({ queryKey: ['networks'], queryFn: dashboardApi.networks, refetchInterval: interval });
}
