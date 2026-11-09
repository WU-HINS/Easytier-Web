import { useMutation, useQuery } from '@tanstack/react-query';
import { networksApi } from './api';

/**
 * The networks feature's hooks. Query keys are declared here and nowhere else, so
 * every invalidation in this module names the same string.
 */
export function useNetworks(interval: number | false = false) {
  return useQuery({ queryKey: ['networks'], queryFn: networksApi.list, refetchInterval: interval });
}

export function useNetwork(networkID: string, options?: { enabled?: boolean }) {
  return useQuery({
    queryKey: ['network', networkID],
    queryFn: () => networksApi.get(networkID),
    enabled: options?.enabled ?? true,
  });
}

export function useNetworkMembers(networkID: string) {
  return useQuery({ queryKey: ['network-members', networkID], queryFn: () => networksApi.members(networkID) });
}

export function useNetworkAdvanced(networkID: string) {
  return useQuery({ queryKey: ['network-advanced', networkID], queryFn: () => networksApi.advanced(networkID) });
}

export function useAclPolicy(networkID: string) {
  return useQuery({ queryKey: ['acl-policy', networkID], queryFn: () => networksApi.aclPolicy(networkID) });
}

export function useCredentials(networkID: string) {
  return useQuery({ queryKey: ['credentials', networkID], queryFn: () => networksApi.credentials(networkID) });
}

export function useMemberToml(networkID: string, deviceID: string) {
  return useQuery({
    queryKey: ['member-toml', networkID, deviceID],
    queryFn: () => networksApi.memberToml(networkID, deviceID),
  });
}

export function useMemberConfig(networkID: string, deviceID: string) {
  return useQuery({
    queryKey: ['member-config', networkID, deviceID],
    queryFn: () => networksApi.memberConfig(networkID, deviceID),
  });
}

export function useInstanceInfo(machineID: string, instanceID: string, options?: { enabled?: boolean }) {
  return useQuery({
    queryKey: ['instance-info', machineID, instanceID],
    queryFn: () => networksApi.instanceInfo(machineID, instanceID),
    enabled: options?.enabled ?? true,
  });
}

export function useAclStats(machineID: string, instanceID: string) {
  return useQuery({
    queryKey: ['acl-stats', machineID, instanceID],
    queryFn: () => networksApi.aclStats(machineID, instanceID),
  });
}

export function useLoggerLevel(machineID: string) {
  return useQuery({ queryKey: ['logger-level', machineID], queryFn: () => networksApi.loggerLevel(machineID) });
}

export function useCreateNetwork(options: { onSuccess?: (id: string) => void; onError?: (failure: Error) => void }) {
  return useMutation({
    mutationFn: (settings: Parameters<typeof networksApi.create>[0]) => networksApi.create(settings),
    onSuccess: (result) => options.onSuccess?.(result.network_id),
    onError: options.onError,
  });
}

export function useUpdateNetwork(options: { onSuccess?: () => void; onError?: (failure: Error) => void }) {
  return useMutation({
    mutationFn: (input: { networkID: string; patch: Record<string, unknown> }) =>
      networksApi.update(input.networkID, input.patch),
    onSuccess: options.onSuccess,
    onError: options.onError,
  });
}

export function useDeleteNetwork(options: { onSuccess?: () => void; onError?: (failure: Error) => void }) {
  return useMutation({
    mutationFn: (networkID: string) => networksApi.remove(networkID),
    onSuccess: options.onSuccess,
    onError: options.onError,
  });
}

export function useRotateSecret(options: { onSuccess?: (secret: string) => void; onError?: (failure: Error) => void }) {
  return useMutation({
    mutationFn: (networkID: string) => networksApi.rotateSecret(networkID),
    onSuccess: (result) => options.onSuccess?.(result.network_secret),
    onError: options.onError,
  });
}

export function useSaveAdvanced(options: { onSuccess?: () => void; onError?: (failure: Error) => void }) {
  return useMutation({
    mutationFn: (input: { networkID: string; advanced: string }) =>
      networksApi.saveAdvanced(input.networkID, input.advanced),
    onSuccess: options.onSuccess,
    onError: options.onError,
  });
}

export function useSaveAclPolicy(options: { onSuccess?: () => void; onError?: (failure: Error) => void }) {
  return useMutation({
    mutationFn: (input: { networkID: string; policy: string }) =>
      networksApi.saveAclPolicy(input.networkID, input.policy),
    onSuccess: options.onSuccess,
    onError: options.onError,
  });
}

export function useIssueCredential(options: { onSuccess?: (secret: string) => void; onError?: (failure: Error) => void }) {
  return useMutation({
    mutationFn: (input: { networkID: string; payload: Record<string, unknown> }) =>
      networksApi.createCredential(input.networkID, input.payload),
    onSuccess: (result) => options.onSuccess?.(result.credential_secret),
    onError: options.onError,
  });
}

export function useRevokeCredential(options: { onSuccess?: () => void; onError?: (failure: Error) => void }) {
  return useMutation({
    mutationFn: (input: { networkID: string; credentialID: string }) =>
      networksApi.revokeCredential(input.networkID, input.credentialID),
    onSuccess: options.onSuccess,
    onError: options.onError,
  });
}

export function useAddMembers(options: { onSuccess?: () => void; onError?: (failure: Error) => void }) {
  return useMutation({
    mutationFn: (input: { networkID: string; deviceIDs: string[] }) =>
      networksApi.addMembers(input.networkID, input.deviceIDs),
    onSuccess: options.onSuccess,
    onError: options.onError,
  });
}

export function useRemoveMember(options: { onSuccess?: () => void; onError?: (failure: Error) => void }) {
  return useMutation({
    mutationFn: (input: { networkID: string; deviceID: string }) =>
      networksApi.removeMember(input.networkID, input.deviceID),
    onSuccess: options.onSuccess,
    onError: options.onError,
  });
}

export function usePatchMember(options: { onSuccess?: () => void; onError?: (failure: Error) => void }) {
  return useMutation({
    mutationFn: (input: {
      networkID: string;
      deviceID: string;
      patch: { hostname_override?: string; virtual_ipv4?: string };
    }) => networksApi.patchMember(input.networkID, input.deviceID, input.patch),
    onSuccess: options.onSuccess,
    onError: options.onError,
  });
}

export function useSaveMemberConfig(options: { onSuccess?: () => void; onError?: (failure: Error) => void }) {
  return useMutation({
    mutationFn: (input: { networkID: string; deviceID: string; override: string }) =>
      networksApi.saveMemberConfig(input.networkID, input.deviceID, input.override),
    onSuccess: options.onSuccess,
    onError: options.onError,
  });
}

export function useSetLoggerLevel(options: { onSuccess?: () => void; onError?: (failure: Error) => void }) {
  return useMutation({
    mutationFn: (input: { machineID: string; level: string }) => networksApi.setLoggerLevel(input.machineID, input.level),
    onSuccess: options.onSuccess,
    onError: options.onError,
  });
}
