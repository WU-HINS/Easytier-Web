import { useMutation, useQuery } from '@tanstack/react-query';
import { devicesApi } from './api';

export function useMachines(interval: number | false = false) {
  return useQuery({ queryKey: ['machines'], queryFn: devicesApi.machines, refetchInterval: interval });
}

export function useBlockedMachines() {
  return useQuery({ queryKey: ['blocked-machines'], queryFn: devicesApi.blocked });
}

export function useRpcCapabilities() {
  return useQuery({ queryKey: ['rpc-capabilities'], queryFn: devicesApi.capabilities, staleTime: 60_000 });
}

export function useMachineRoutes(
  machineID: string,
  instance: string | null,
  options?: { enabled?: boolean },
) {
  return useQuery({
    queryKey: ['routes', machineID, instance],
    queryFn: () => devicesApi.routes(machineID, instance as string),
    enabled: options?.enabled ?? Boolean(instance),
    retry: false,
  });
}

export function useRenameMachine(options: { onSuccess?: () => void; onError?: (failure: Error) => void }) {
  return useMutation({
    mutationFn: (input: { machineID: string; alias: string }) => devicesApi.rename(input.machineID, input.alias),
    onSuccess: options.onSuccess,
    onError: options.onError,
  });
}

export function useDeleteMachine(options: { onSuccess?: () => void; onError?: (failure: Error) => void }) {
  return useMutation({
    mutationFn: (machineID: string) => devicesApi.remove(machineID),
    onSuccess: options.onSuccess,
    onError: options.onError,
  });
}

export function useUnblockMachine(options: { onSuccess?: () => void; onError?: (failure: Error) => void }) {
  return useMutation({
    mutationFn: (machineID: string) => devicesApi.unblock(machineID),
    onSuccess: options.onSuccess,
    onError: options.onError,
  });
}
