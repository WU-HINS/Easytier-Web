import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { copy } from '../../copy/zh-CN';
import { api } from '../../lib/http';
import { Button, Card, CardBody, Skeleton } from '../../ui';
import { toMemberOverrides } from './toml-overrides';

/**
 * The effective configuration of one member, shown as TOML. The text comes from
 * the same compiler that pushes to the device, so what an operator reads here is
 * what the device will be told, not a reconstruction of the panel state.
 */
export function MemberTomlPanel({ networkID, deviceID }: { networkID: string; deviceID: string }) {
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState<string | null>(null);
  const [message, setMessage] = useState('');
  const [copied, setCopied] = useState(false);
  const toml = useQuery({
    queryKey: ['member-toml', networkID, deviceID],
    queryFn: () => api.memberToml(networkID, deviceID),
  });

  useEffect(() => {
    if (toml.data && toml.data.toml_config !== undefined) setDraft(toml.data.toml_config);
  }, [toml.data]);

  const apply = useMutation({
    mutationFn: async (text: string) => {
      const parsed = await api.parseConfig(text);
      if (parsed.error) throw new Error(parsed.error);
      // An override takes a subset of the config in snake_case; sending the whole
      // parsed config is rejected with an unknown field error.
      await api.putMemberConfig(networkID, deviceID, JSON.stringify(toMemberOverrides(parsed.config)));
    },
    onSuccess: () => {
      setMessage(copy.networks.tomlSaved);
      void queryClient.invalidateQueries({ queryKey: ['member-config', networkID, deviceID] });
      void queryClient.invalidateQueries({ queryKey: ['network-members', networkID] });
    },
    onError: (failure: Error) => setMessage(copy.networks.tomlInvalid + ': ' + failure.message),
  });

  const current = draft === null ? '' : draft;

  return (
    <Card>
      <CardBody className="space-y-[var(--u-3)]">
        <div>
          <h3 className="text-phi-base font-semibold">{copy.networks.tomlTitle}</h3>
          <p className="mt-[var(--u-1)] text-phi-sm text-muted-foreground">{copy.networks.tomlHint}</p>
        </div>
        {toml.isLoading ? <Skeleton className="h-[var(--u-16)]" /> : null}
        {toml.data && toml.data.error ? (
          <p role="alert" className="text-phi-sm text-destructive">{toml.data.error}</p>
        ) : null}
        {!toml.isLoading && !(toml.data && toml.data.error) ? (
          <>
            <textarea
              aria-label={copy.networks.tomlTitle}
              value={current}
              rows={12}
              spellCheck={false}
              onChange={(event) => setDraft(event.target.value)}
              className="w-full rounded-[var(--radius)] border border-input bg-card p-[var(--u-3)] font-mono text-phi-sm text-foreground"
            />
            <div className="flex flex-wrap items-center gap-[var(--u-2)]">
              <Button type="button" disabled={apply.isPending} onClick={() => apply.mutate(current)}>
                {copy.networks.tomlApply}
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
                {copied ? copy.networks.tomlCopied : copy.networks.tomlCopy}
              </Button>
              {message ? <span className="text-phi-sm text-muted-foreground">{message}</span> : null}
              <Button
                type="button"
                variant="ghost"
                onClick={() => {
                  const blob = new Blob([current], { type: "text/plain;charset=utf-8" });
                  const url = URL.createObjectURL(blob);
                  const link = document.createElement("a");
                  link.href = url;
                  link.download = (deviceID || "member") + ".toml";
                  link.click();
                  URL.revokeObjectURL(url);
                  setMessage(copy.networks.exportDone);
                }}
              >
                {copy.networks.exportConfig}
              </Button>
            </div>
          </>
        ) : null}
      </CardBody>
    </Card>
  );
}
