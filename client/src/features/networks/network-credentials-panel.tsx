import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { useCopy } from '../../copy/locale';
import { type NetworkCredential } from '../../lib/http';
import { Badge, Button, Card, CardBody, Skeleton } from '../../ui';
import { describeError } from '../../lib/errors';
import { networksApi } from './api';

// expiryLabel renders 0 as permanent and anything else as a local timestamp.
function expiryLabel(credential: NetworkCredential, copy: ReturnType<typeof useCopy>): string {
  if (!credential.expiry_unix) return copy.networkSettings.forever;
  return new Date(credential.expiry_unix * 1000).toLocaleString('zh-CN', { hour12: false });
}

/**
 * Admission credentials on their own tab. The table carries the same columns the
 * first console showed: the groups a credential belongs to, whether it may relay,
 * whether it can be reused, and when it expires.
 */
export function NetworkCredentialsPanel({ networkID }: { networkID: string }) {
  const copy = useCopy();
  const queryClient = useQueryClient();
  const [secret, setSecret] = useState('');
  const [copied, setCopied] = useState(false);
  const credentials = useQuery({ queryKey: ['credentials', networkID], queryFn: () => networksApi.credentials(networkID) });
  const network = useQuery({ queryKey: ['network', networkID], queryFn: () => networksApi.get(networkID) });

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['credentials', networkID] });
  };
  const issue = useMutation({
    mutationFn: () => networksApi.createCredential(networkID, { reusable: true }),
    onSuccess: (created) => {
      setSecret(created.credential_secret);
      refresh();
    },
  });
  const revoke = useMutation({
    mutationFn: (credentialID: string) => networksApi.revokeCredential(networkID, credentialID),
    onSuccess: () => {
      setSecret('');
      refresh();
    },
  });

  const networkName = network.data ? network.data.network_name : '';
  const joinCommand =
    secret && networkName
      ? "easytier-core -d --network-name " + networkName + " --network-secret " + secret
      : '';
  const copyJoin = async () => {
    try {
      await navigator.clipboard.writeText(joinCommand);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  };

  const issued = credentials.data ? credentials.data.credentials : [];

  return (
    <Card>
      <CardBody className="space-y-[var(--u-3)]">
        <div className="flex flex-wrap items-center justify-between gap-[var(--u-3)]">
          <h2 className="text-phi-xl font-semibold">{copy.networkSettings.credentials}</h2>
          <Button type='button' disabled={issue.isPending} onClick={() => issue.mutate()}>
            {copy.networkSettings.issue}
          </Button>
        </div>
        {credentials.isLoading ? <Skeleton className="h-[var(--u-10)]" /> : null}
        {secret ? (
          <div className="space-y-[var(--u-2)]">
            <p className="break-all text-phi-sm text-muted-foreground">
              {copy.networkSettings.secretOnce}: <span className="font-mono text-foreground">{secret}</span>
            </p>
            <p className="text-phi-sm text-muted-foreground">{copy.networks.joinCommand}</p>
            <div className="flex flex-wrap items-center gap-[var(--u-2)]">
              <code className="min-w-0 flex-1 break-all rounded-[var(--radius)] border border-border bg-card p-[var(--u-2)] font-mono text-phi-sm">
                {joinCommand}
              </code>
              <Button type="button" variant="ghost" onClick={copyJoin}>
                {copied ? copy.networks.overrideCopied : copy.networks.joinCopy}
              </Button>
            </div>
          </div>
        ) : null}
        {issued.length === 0 ? (
          <p className="text-phi-sm text-muted-foreground">{copy.networkSettings.noCredentials}</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full border-collapse text-left text-phi-sm">
              <thead>
                <tr className="border-b border-border text-muted-foreground">
                  <th className="py-[var(--u-3)] pr-[var(--u-3)] font-medium">{copy.networkSettings.columnId}</th>
                  <th className="py-[var(--u-3)] pr-[var(--u-3)] font-medium">{copy.networkSettings.columnAcl}</th>
                  <th className="py-[var(--u-3)] pr-[var(--u-3)] font-medium">{copy.networkSettings.columnRelay}</th>
                  <th className="py-[var(--u-3)] pr-[var(--u-3)] font-medium">{copy.networkSettings.columnReusable}</th>
                  <th className="py-[var(--u-3)] pr-[var(--u-3)] font-medium">{copy.networkSettings.columnExpiry}</th>
                  <th className="py-[var(--u-3)] text-right font-medium">{copy.networkSettings.columnActions}</th>
                </tr>
              </thead>
              <tbody>
                {issued.map((credential) => (
                  <tr key={credential.credential_id} className="border-b border-border last:border-0">
                    <td className="py-[var(--u-3)] pr-[var(--u-3)] break-all font-mono">{credential.credential_id}</td>
                    <td className="py-[var(--u-3)] pr-[var(--u-3)]">
                      {credential.acl_groups && credential.acl_groups.length > 0
                        ? credential.acl_groups.join(', ')
                        : '—'}
                    </td>
                    <td className="py-[var(--u-3)] pr-[var(--u-3)]">
                      <Badge tone={credential.allow_relay ? 'success' : 'neutral'}>
                        {credential.allow_relay ? copy.networkSettings.yes : copy.networkSettings.no}
                      </Badge>
                    </td>
                    <td className="py-[var(--u-3)] pr-[var(--u-3)]">
                      {credential.reusable ? copy.networkSettings.yes : copy.networkSettings.no}
                    </td>
                    <td className="py-[var(--u-3)] pr-[var(--u-3)] tabular-nums">{expiryLabel(credential, copy)}</td>
                    <td className="py-[var(--u-3)] text-right">
                      <Button type='button' variant='ghost' onClick={() => revoke.mutate(credential.credential_id)}>
                        {copy.networkSettings.revoke}
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {issue.isError ? (
          <p role='alert' className="text-phi-sm text-destructive">
            {describeError(issue.error, copy)}
          </p>
        ) : null}
      </CardBody>
    </Card>
  );
}
