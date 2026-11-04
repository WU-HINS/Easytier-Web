import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { copy } from '../../copy/zh-CN';
import { api } from '../../lib/http';
import { Button, Card, CardBody, Input, Skeleton } from '../../ui';
import { NetworkAdvanced } from './network-advanced';

/**
 * Name, access control list and admission credentials for one network. The ACL
 * is edited as raw JSON because the policy language belongs to the agent; the
 * server validates it on write and rejects anything the compiler cannot parse.
 */
export function NetworkSettings({ networkID, displayName }: { networkID: string; displayName: string }) {
  const queryClient = useQueryClient();
  const [name, setName] = useState(displayName);
  const [policy, setPolicy] = useState('');
  const [textAreaKey, setTextAreaKey] = useState(0);
  const [secret, setSecret] = useState('');

  useEffect(() => {
    setName(displayName);
  }, [displayName]);

  const acl = useQuery({ queryKey: ['acl', networkID], queryFn: () => api.aclPolicy(networkID) });
  const credentials = useQuery({ queryKey: ['credentials', networkID], queryFn: () => api.credentials(networkID) });

  useEffect(() => {
    if (acl.data) {
      setPolicy(acl.data.acl_policy ?? '');
      setTextAreaKey((key) => key + 1);
    }
  }, [acl.data]);

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['networks'] });
    void queryClient.invalidateQueries({ queryKey: ['acl', networkID] });
    void queryClient.invalidateQueries({ queryKey: ['credentials', networkID] });
  };

  const rename = useMutation({
    mutationFn: () => api.updateNetwork(networkID, { display_name: name.trim() }),
    onSuccess: refresh,
  });
  const savePolicy = useMutation({
    mutationFn: () => api.putAclPolicy(networkID, policy),
    onSuccess: refresh,
  });
  const issue = useMutation({
    mutationFn: () => api.createCredential(networkID, { reusable: true }),
    onSuccess: (created) => {
      setSecret(created.credential_secret);
      refresh();
    },
  });
  const revoke = useMutation({
    mutationFn: (credentialID: string) => api.revokeCredential(networkID, credentialID),
    onSuccess: () => {
      setSecret('');
      refresh();
    },
  });

  const issued = credentials.data?.credentials ?? [];

  return (
    <>
    <Card>
      <CardBody className="space-y-[var(--gap-card)]">
        <section>
          <h2 className="text-phi-lg font-semibold">{copy.networkSettings.title}</h2>
          <div className="mt-[var(--u-4)] flex flex-wrap items-end gap-[var(--u-3)]">
            <label className="min-w-0 flex-1">
              <span className="mb-[var(--u-2)] block text-phi-sm text-muted-foreground">
                {copy.networkSettings.name}
              </span>
              <Input value={name} onChange={(event) => setName(event.target.value)} />
            </label>
            <Button
              type="button"
              disabled={!name.trim() || rename.isPending}
              onClick={() => rename.mutate()}
            >
              {copy.common.save}
            </Button>
          </div>
          {rename.isError ? (
            <p role="alert" className="mt-[var(--u-2)] text-phi-sm text-destructive">{(rename.error as Error).message}</p>
          ) : null}
        </section>

        <section>
          <h3 className="text-phi-base font-semibold">{copy.networkSettings.acl}</h3>
          <p className="mt-[var(--u-2)] text-phi-sm text-muted-foreground">{copy.networkSettings.aclHint}</p>
          {acl.isLoading ? <Skeleton className="mt-[var(--u-3)] h-[var(--u-16)]" /> : null}
          <textarea
            aria-label={copy.networkSettings.acl}
            key={textAreaKey}
            value={policy}
            onChange={(event) => setPolicy(event.target.value)}
            spellCheck={false}
            rows={6}
            className="mt-[var(--u-3)] w-full rounded-[var(--radius)] border border-input bg-card p-[var(--u-3)] font-mono text-phi-sm text-foreground"
          />
          <div className="mt-[var(--u-3)] flex items-center gap-[var(--u-2)]">
            <Button type="button" disabled={savePolicy.isPending} onClick={() => savePolicy.mutate()}>
              {copy.networkSettings.saveAcl}
            </Button>
            {savePolicy.isError ? (
              <span className="text-phi-sm text-destructive">{(savePolicy.error as Error).message}</span>
            ) : null}
            {savePolicy.isSuccess ? (
              <span className="text-phi-sm text-muted-foreground">{copy.networkSettings.saved}</span>
            ) : null}
          </div>
        </section>

        <section>
          <div className="flex flex-wrap items-center justify-between gap-[var(--u-3)]">
            <h3 className="text-phi-base font-semibold">{copy.networkSettings.credentials}</h3>
            <Button type="button" disabled={issue.isPending} onClick={() => issue.mutate()}>
              {copy.networkSettings.issue}
            </Button>
          </div>
          {secret ? (
            <p className="mt-[var(--u-3)] break-all text-phi-sm text-muted-foreground">
              {copy.networkSettings.secretOnce}: <span className="font-mono text-foreground">{secret}</span>
            </p>
          ) : null}
          <div className="mt-[var(--u-3)] space-y-[var(--u-2)]">
            {issued.length === 0 ? (
              <p className="text-phi-sm text-muted-foreground">{copy.networkSettings.noCredentials}</p>
            ) : null}
            {issued.map((credential) => (
              <div
                key={credential.credential_id}
                className="flex flex-wrap items-center justify-between gap-[var(--u-3)] border-b border-border pb-[var(--u-2)] last:border-0"
              >
                <span className="break-all font-mono text-phi-sm">{credential.credential_id}</span>
                <Button type="button" variant="ghost" onClick={() => revoke.mutate(credential.credential_id)}>
                  {copy.networkSettings.revoke}
                </Button>
              </div>
            ))}
          </div>
          {issue.isError ? (
            <p role="alert" className="mt-[var(--u-2)] text-phi-sm text-destructive">{(issue.error as Error).message}</p>
          ) : null}
        </section>
      </CardBody>
    </Card>
    <NetworkAdvanced networkID={networkID} />
    </>
  );
}
