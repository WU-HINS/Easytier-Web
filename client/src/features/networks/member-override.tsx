import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { copy } from '../../copy/zh-CN';
import { api } from '../../lib/http';
import { Button } from '../../ui';
import { MemberTomlPanel } from './member-toml-panel';

/**
 * Per member override blob. The endpoint existed from the start but nothing in
 * the console could reach it, so an operator had to edit the database to change
 * one device's config.
 */
export function MemberOverride({ networkID, deviceID }: { networkID: string; deviceID: string }) {
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  const config = useQuery({
    queryKey: ['member-config', networkID, deviceID],
    queryFn: () => api.memberConfig(networkID, deviceID),
  });

  const save = useMutation({
    mutationFn: (value: string) => api.putMemberConfig(networkID, deviceID, value),
    onSuccess: () => {
      setDraft(null);
      setError(null);
      void queryClient.invalidateQueries({ queryKey: ['member-config', networkID, deviceID] });
      void queryClient.invalidateQueries({ queryKey: ['network-members', networkID] });
    },
    onError: (failure: Error) => setError(failure.message),
  });

  if (config.isLoading) {
    return <p className="text-phi-sm text-muted-foreground">{copy.networks.overrideLoading}</p>;
  }

  const current = draft ?? config.data ?? '';
  const submit = () => {
    const value = current.trim();
    if (value !== '') {
      try {
        JSON.parse(value);
      } catch {
        setError(copy.networks.overrideInvalid);
        return;
      }
    }
    setError(null);
    save.mutate(value);
  };

  return (
    <div className="mt-[var(--u-3)] space-y-[var(--u-2)]">
      <textarea
        aria-label={copy.networks.overrideTitle}
        value={current}
        rows={4}
        placeholder="{ }"
        onChange={(event) => setDraft(event.target.value)}
        className="w-full rounded-[var(--radius)] border border-input bg-card p-[var(--u-3)] font-mono text-phi-sm text-foreground"
      />
      <div className="flex flex-wrap items-center gap-[var(--u-2)]">
        <Button type="button" disabled={save.isPending} onClick={submit}>
          {copy.common.save}
        </Button>
        <Button
          type="button"
          variant="ghost"
          disabled={save.isPending}
          onClick={() => {
            setDraft('');
            setError(null);
            save.mutate('');
          }}
        >
          {copy.networks.overrideClear}
        </Button>
        <Button
          type="button"
          variant="ghost"
          onClick={async () => {
            try {
              await navigator.clipboard.writeText(current);
              setCopied(true);
            } catch {
              setCopied(false);
            }
          }}
        >
          {copied ? copy.networks.overrideCopied : copy.networks.overrideCopy}
        </Button>
        <span className="text-phi-sm text-muted-foreground">{copy.networks.overrideHint}</span>
      </div>
      {error ? <p role="alert" className="text-phi-sm text-destructive">{error}</p> : null}
      <MemberTomlPanel networkID={networkID} deviceID={deviceID} />
      {config.isError ? (
        <p role="alert" className="text-phi-sm text-destructive">{(config.error as Error).message}</p>
      ) : null}
    </div>
  );
}
