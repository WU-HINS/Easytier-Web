import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { copy } from '../../copy/zh-CN';
import { api, type AdminUser } from '../../lib/http';
import { Badge, Button, Card, CardBody, Skeleton } from '../../ui';

/**
 * Read-only user administration: the console never exposes password, credential
 * or binding writes here, by design.
 */
export function AdminUsersPage() {
  const queryClient = useQueryClient();
  const users = useQuery({ queryKey: ['admin-users'], queryFn: api.adminUsers });
  const [error, setError] = useState<string | null>(null);

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['admin-users'] });

  const toggle = useMutation({
    mutationFn: ({ user }: { user: AdminUser }) => api.setUserDisabled(user.id, !user.disabled),
    onSuccess: invalidate,
    onError: (failure: Error) => setError(failure.message),
  });

  const remove = useMutation({
    mutationFn: (user: AdminUser) => api.deleteUser(user.id),
    onSuccess: invalidate,
    onError: (failure: Error) => setError(failure.message),
  });

  const rows = users.data ?? [];

  return (
    <div className="space-y-[var(--gap-section)]">
      <header>
        <h1 className="text-phi-3xl font-semibold tracking-tight">{copy.adminUsers.title}</h1>
        <p className="mt-[var(--u-2)] text-phi-sm text-muted-foreground">
          {copy.adminUsers.hint}
        </p>
      </header>
      {error ? <p role="alert" className="text-phi-sm text-destructive">{error}</p> : null}
      <Card>
        <CardBody className="p-0">
          {users.isLoading ? <div className="p-[var(--pad-card)]"><Skeleton className="h-[var(--u-12)]" /></div> : null}
          {rows.length === 0 && !users.isLoading ? (
            <p className="p-[var(--pad-card)] text-phi-sm text-muted-foreground">{copy.adminUsers.empty}</p>
          ) : null}
          {rows.length > 0 ? (
            <div className="hidden md:block">
            <table className="w-full border-collapse text-left text-phi-base">
              <thead>
                <tr className="border-b border-border text-phi-sm text-muted-foreground">
                  <th className="px-[var(--pad-card)] py-[var(--u-4)] font-medium">{copy.adminUsers.columnUser}</th>
                  <th className="py-[var(--u-4)] font-medium">{copy.adminUsers.columnRole}</th>
                  <th className="py-[var(--u-4)] font-medium">{copy.adminUsers.columnDevices}</th>
                  <th className="py-[var(--u-4)] font-medium">OIDC</th>
                  <th className="px-[var(--pad-card)] py-[var(--u-4)] text-right font-medium">{copy.adminUsers.columnActions}</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((user) => (
                  <tr key={user.id} className="border-b border-border last:border-0">
                    <td className="px-[var(--pad-card)] py-[var(--u-4)]">
                      <span className="font-medium">{user.username}</span>
                      {user.disabled ? <Badge tone="danger">{copy.adminUsers.disabledBadge}</Badge> : null}
                    </td>
                    <td className="py-[var(--u-4)]">
                      <Badge tone={user.role === 'admin' ? 'primary' : 'neutral'}>{user.role}</Badge>
                    </td>
                    <td className="py-[var(--u-4)] tabular-nums text-muted-foreground">
                      {user.device_count} / {user.online_instances}
                    </td>
                    <td className="py-[var(--u-4)]">
                      <Badge tone={user.oidc_bound ? 'success' : 'neutral'}>{user.oidc_bound ? copy.adminUsers.boundOidc : copy.adminUsers.unboundOidc}</Badge>
                    </td>
                    <td className="px-[var(--pad-card)] py-[var(--u-4)] text-right">
                      {user.role === 'admin' ? (
                        <span className="text-phi-sm text-muted-foreground">{copy.adminUsers.protectedBadge}</span>
                      ) : (
                        <div className="flex justify-end gap-[var(--u-2)]">
                          <Button size="sm" variant="secondary" onClick={() => toggle.mutate({ user })}>
                            {user.disabled ? copy.adminUsers.enable : copy.adminUsers.disable}
                          </Button>
                          <Button
                            size="sm"
                            variant="danger"
                            disabled={user.online_instances > 0}
                            title={user.online_instances > 0 ? copy.adminUsers.removeBlocked : undefined}
                            onClick={() => remove.mutate(user)}
                          >
                            {copy.adminUsers.remove}
                          </Button>
                        </div>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            </div>
          ) : null}
          {rows.length > 0 ? (
            <div className="divide-y divide-border md:hidden">
              {rows.map((user) => (
                <div key={user.id} className="space-y-[var(--u-2)] px-[var(--pad-card)] py-[var(--u-4)]">
                  <div className="flex items-center justify-between gap-[var(--u-3)]">
                    <span className="truncate font-medium">{user.username}</span>
                    <Badge tone={user.role === 'admin' ? 'primary' : 'neutral'}>{user.role}</Badge>
                  </div>
                  <p className="text-phi-sm text-muted-foreground">
                    {user.device_count}{copy.adminUsers.deviceSuffix} · {user.online_instances}{copy.adminUsers.onlineInstanceSuffix} · {user.oidc_bound ? copy.adminUsers.boundOidc : copy.adminUsers.unboundOidc}
                  </p>
                  {user.role === 'admin' ? (
                    <span className="text-phi-sm text-muted-foreground">{copy.adminUsers.protectedAccount}</span>
                  ) : (
                    <div className="flex gap-[var(--u-2)]">
                      <Button size="sm" variant="secondary" onClick={() => toggle.mutate({ user })}>
                        {user.disabled ? copy.adminUsers.enable : copy.adminUsers.disable}
                      </Button>
                      <Button size="sm" variant="danger" disabled={user.online_instances > 0}
                        onClick={() => remove.mutate(user)}>{copy.adminUsers.remove}</Button>
                    </div>
                  )}
                </div>
              ))}
            </div>
          ) : null}
        </CardBody>
      </Card>
      <p className="text-phi-sm text-muted-foreground">{copy.common.loading.replace('…', '')}{copy.adminUsers.refreshHint}</p>
    </div>
  );
}
