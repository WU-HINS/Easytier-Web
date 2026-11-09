import { useMutation } from '@tanstack/react-query';
import { setupApi } from './api';

export function useCreateAdmin(options?: { onSuccess?: () => void; onError?: (failure: Error) => void }) {
  return useMutation({
    mutationFn: (input: { username: string; password: string }) =>
      setupApi.createAdmin(input.username, input.password),
    onSuccess: options?.onSuccess,
    onError: options?.onError,
  });
}
