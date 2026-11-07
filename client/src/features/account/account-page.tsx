import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { useCopy } from '../../copy/locale';
import { api, isSecureOrigin, oidcCallbackURL } from '../../lib/http';
import { Badge, Button, Card, CardBody } from '../../ui';
import { PasswordCard } from './password-card';

/**
 * Self-service OIDC binding. Binding always goes through a browser redirect, and
 * the callback URL is produced here (never by the API) and must be https.
 */
export function AccountPage() {
  const copy = useCopy();
  const queryClient = useQueryClient();
  const oidc = useQuery({ queryKey: ['account-oidc'], queryFn: api.accountOidc });
  const config = useQuery({ queryKey: ['oidc-config'], queryFn: api.oidcConfig });
  const [error, setError] = useState<string | null>(null);

  const unbind = useMutation({
    mutationFn: api.unbindOidc,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['account-oidc'] }),
    onError: (failure: Error) => setError(failure.message),
  });

  const secure = isSecureOrigin();
  const loading = oidc.isLoading || config.isLoading;
  const failed = oidc.isError || config.isError;
  const enabled = config.data?.enabled === true;
  const bound = oidc.data?.bound === true;

  const bind = () => {
    if (!secure) {
      setError(copy.account.requiresHttps);
      return;
    }
    const target = new URL('/api/v1/account/oidc/bind', window.location.origin);
    target.searchParams.set('redirect_url', oidcCallbackURL());
    window.location.assign(target.toString());
  };

  return (
    <div className="space-y-[var(--gap-section)]">
      <header>
        <h1 className="text-phi-2xl font-semibold tracking-tight">{copy.account.title}</h1>
        <p className="mt-[var(--u-2)] text-phi-sm text-muted-foreground">{copy.account.subtitle}</p>
      </header>

      {loading ? <p className="text-phi-sm text-muted-foreground">{copy.account.loading}</p> : null}
      {failed ? <p role="alert" className="text-phi-sm text-destructive">{copy.account.failed}</p> : null}

      <Card className="max-w-[calc(var(--u-12)*9)]">
        <CardBody className="space-y-[var(--u-4)]">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-phi-base font-medium">{copy.account.section}</p>
              <p className="text-phi-sm text-muted-foreground">
                {copy.account.boundTo(oidc.data?.issuer ?? '')}
              </p>
            </div>
            <Badge tone={bound ? 'success' : 'neutral'}>{bound ? copy.account.bound : copy.account.unbound}</Badge>
          </div>

          <div className="flex items-center gap-[var(--u-3)]">
            {bound ? (
              <Button variant="secondary" onClick={() => unbind.mutate()} disabled={unbind.isPending}>{copy.account.unbind}</Button>
            ) : (
              <Button onClick={bind} disabled={!enabled}>{copy.account.bind}</Button>
            )}
            {!enabled ? <span className="text-phi-sm text-muted-foreground">{copy.account.oidcDisabled}</span> : null}
          </div>

          <div className="rounded-md bg-muted px-[var(--u-4)] py-[var(--u-3)] text-phi-sm">
            <p className="text-muted-foreground">{copy.account.callbackLabel}</p>
            <code className="break-all">{oidcCallbackURL()}</code>
            {!secure ? (
              <p role="alert" className="mt-[var(--u-2)] text-destructive">{copy.account.insecure}</p>
            ) : null}
          </div>
          {error ? <p role="alert" className="text-phi-sm text-destructive">{error}</p> : null}
        </CardBody>
      </Card>
      <PasswordCard />
</div>
  );
}
