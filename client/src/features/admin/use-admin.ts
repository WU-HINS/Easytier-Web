import { useMutation, useQuery } from '@tanstack/react-query';
import { adminApi } from './api';

export function useAdminUsers() {
  return useQuery({ queryKey: ['admin-users'], queryFn: adminApi.users });
}

export function useSetUserDisabled(options: {
  onSuccess?: () => void;
  onError?: (failure: Error) => void;
}) {
  return useMutation({
    mutationFn: (user: { id: number; disabled: boolean }) => adminApi.setUserDisabled(user.id, !user.disabled),
    onSuccess: options.onSuccess,
    onError: options.onError,
  });
}

export function useDeleteUser(options: { onSuccess?: () => void; onError?: (failure: Error) => void }) {
  return useMutation({
    mutationFn: (id: number) => adminApi.deleteUser(id),
    onSuccess: options.onSuccess,
    onError: options.onError,
  });
}

export function useAdminSettings() {
  return useQuery({ queryKey: ['admin-settings'], queryFn: adminApi.settings });
}

export function useSaveAdminSettings(options: {
  onSuccess?: (next: Awaited<ReturnType<typeof adminApi.saveSettings>>) => void;
  onError?: (failure: Error) => void;
}) {
  return useMutation({
    mutationFn: (patch: Record<string, unknown>) => adminApi.saveSettings(patch),
    onSuccess: options.onSuccess,
    onError: options.onError,
  });
}
