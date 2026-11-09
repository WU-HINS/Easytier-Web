import { useMutation, useQuery } from '@tanstack/react-query';
import { accountApi } from './api';

export function useOidcBinding() {
  return useQuery({ queryKey: ['account-oidc'], queryFn: accountApi.oidcBinding });
}

export function useUnbindOidc(options?: { onSuccess?: () => void; onError?: (failure: Error) => void }) {
  return useMutation({ mutationFn: accountApi.unbindOidc, onSuccess: options?.onSuccess, onError: options?.onError });
}

export function useChangePassword(options?: { onSuccess?: () => void; onError?: (failure: Error) => void }) {
  return useMutation({
    mutationFn: (input: { current: string; next: string }) => accountApi.changePassword(input.current, input.next),
    onSuccess: options?.onSuccess,
    onError: options?.onError,
  });
}
