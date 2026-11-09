import { useMutation, useQuery } from '@tanstack/react-query';
import { authApi } from './api';

/**
 * The auth feature's hooks. Query keys are owned here, so an invalidation anywhere
 * can name 'console-info' without knowing which component asked first.
 */
export function useConsoleInfo(options?: { enabled?: boolean; retry?: boolean }) {
  return useQuery({
    queryKey: ['console-info'],
    queryFn: authApi.consoleInfo,
    enabled: options?.enabled,
    retry: options?.retry,
  });
}

export function useOidcConfig() {
  return useQuery({ queryKey: ['oidc-config'], queryFn: authApi.oidcConfig, staleTime: 60_000 });
}

export function useSetupStatus() {
  return useQuery({ queryKey: ['setup'], queryFn: authApi.setupStatus, staleTime: 60_000 });
}

export function useLogin(options?: { onSuccess?: () => void; onError?: (failure: Error) => void }) {
  return useMutation({
    mutationFn: (input: { username: string; password: string }) => authApi.login(input.username, input.password),
    onSuccess: options?.onSuccess,
    onError: options?.onError,
  });
}

export function useRegister(options?: { onSuccess?: () => void; onError?: (failure: Error) => void }) {
  return useMutation({
    mutationFn: (input: { username: string; password: string }) => authApi.register(input.username, input.password),
    onSuccess: options?.onSuccess,
    onError: options?.onError,
  });
}

export function useRegisterOidc(options?: { onSuccess?: () => void; onError?: (failure: Error) => void }) {
  return useMutation({
    mutationFn: (username: string) => authApi.registerOidc(username),
    onSuccess: options?.onSuccess,
    onError: options?.onError,
  });
}

export function useLogout(options?: { onSuccess?: (result: { status: string; redirect?: string }) => void }) {
  return useMutation({ mutationFn: authApi.logout, onSuccess: options?.onSuccess });
}
