import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { useCopy } from '../../copy/locale';
import { networksApi } from './api';
import { Button, Card, CardBody, Input } from '../../ui';

// The network level settings: one JSON blob, edited as the handful of fields the
// compiler understands. Members inherit these and may override any of them, so
// this panel is where a cluster is configured once instead of per device.

type Blob = Record<string, unknown>;

function parseBlob(raw: string): Blob {
  if (raw.trim() === '') return {};
  try {
    const parsed = JSON.parse(raw);
    return parsed !== null && typeof parsed === 'object' ? (parsed as Blob) : {};
  } catch {
    return {};
  }
}

function splitList(raw: string): string[] {
  return raw
    .split(',')
    .map((entry) => entry.trim())
    .filter((entry) => entry !== '');
}

export function NetworkAdvanced({ networkID }: { networkID: string }) {
  const copy = useCopy();
  // Labels come from the locale, so the table is built per render.
const switches = [
  { key: 'no_tun', label: copy.networks.advancedSwitchNoTun },
  { key: 'disable_p2p', label: copy.networks.advancedSwitchDisableP2P },
  { key: 'disable_ipv6', label: copy.networks.advancedSwitchDisableIPv6 },
  { key: 'multi_thread', label: copy.networks.advancedSwitchMultiThread },
];
  const queryClient = useQueryClient();
  const advanced = useQuery({ queryKey: ['advanced', networkID], queryFn: () => networksApi.advanced(networkID) });
  const [mtu, setMtu] = useState('');
  const [rateLimit, setRateLimit] = useState('');
  const [listeners, setListeners] = useState('');
  const [whitelist, setWhitelist] = useState('');
  const [enabled, setEnabled] = useState<Record<string, boolean>>({});
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!advanced.data) return;
    const blob = parseBlob(advanced.data.advanced ?? '');
    setMtu(blob.mtu === undefined ? '' : String(blob.mtu));
    setRateLimit(blob.instance_recv_bps_limit === undefined ? '' : String(blob.instance_recv_bps_limit));
    setListeners(Array.isArray(blob.listener_urls) ? (blob.listener_urls as string[]).join(', ') : '');
    setWhitelist(Array.isArray(blob.whitelist) ? (blob.whitelist as string[]).join(', ') : '');
    const next: Record<string, boolean> = {};
    for (const entry of switches) {
      next[entry.key] = blob[entry.key] === true;
    }
    setEnabled(next);
    setSaved(false);
  }, [advanced.data]);

  const buildBlob = () => {
    const blob: Blob = {};
    if (mtu.trim() !== '') blob.mtu = Number(mtu.trim());
    if (rateLimit.trim() !== '') blob.instance_recv_bps_limit = Number(rateLimit.trim());
    const urls = splitList(listeners);
    if (urls.length > 0) blob.listener_urls = urls;
    const allowed = splitList(whitelist);
    if (allowed.length > 0) blob.whitelist = allowed;
    for (const entry of switches) {
      if (enabled[entry.key]) blob[entry.key] = true;
    }
    return JSON.stringify(blob);
  };

  const save = useMutation({
    mutationFn: () => networksApi.saveAdvanced(networkID, buildBlob()),
    onSuccess: () => {
      setError(null);
      setSaved(true);
      void queryClient.invalidateQueries({ queryKey: ['advanced', networkID] });
    },
    onError: (failure: Error) => {
      setSaved(false);
      setError(failure.message || copy.networks.advancedFailed);
    },
  });

  return (
    <Card>
      <CardBody className="space-y-[var(--u-4)]">
        <div>
          <h2 className="text-phi-base font-semibold">{copy.networks.advancedTitle}</h2>
          <p className="mt-[var(--u-1)] text-phi-sm text-muted-foreground">{copy.networks.advancedHint}</p>
        </div>
        <label className="block">
          <span className="mb-[var(--u-2)] block text-phi-sm text-muted-foreground">{copy.networks.advancedMtu}</span>
          <Input value={mtu} onChange={(event) => setMtu(event.target.value)} inputMode="numeric" />
        </label>
        <label className="block">
          <span className="mb-[var(--u-2)] block text-phi-sm text-muted-foreground">{copy.networks.advancedRateLimit}</span>
          <Input value={rateLimit} onChange={(event) => setRateLimit(event.target.value)} inputMode="numeric" />
        </label>
        <label className="block">
          <span className="mb-[var(--u-2)] block text-phi-sm text-muted-foreground">{copy.networks.advancedListeners}</span>
          <Input value={listeners} onChange={(event) => setListeners(event.target.value)} />
        </label>
        <label className="block">
          <span className="mb-[var(--u-2)] block text-phi-sm text-muted-foreground">{copy.networks.advancedWhitelist}</span>
          <Input value={whitelist} onChange={(event) => setWhitelist(event.target.value)} />
        </label>
        <fieldset className="space-y-[var(--u-2)]">
          <legend className="text-phi-sm text-muted-foreground">{copy.networks.advancedSwitches}</legend>
          {switches.map((entry) => (
            <label key={entry.key} className="flex items-center gap-[var(--u-2)] text-phi-sm">
              <input
                type="checkbox"
                checked={enabled[entry.key] === true}
                onChange={(event) => {
                  const checked = event.target.checked;
                  setEnabled((current) => ({ ...current, [entry.key]: checked }));
                }}
              />
              {entry.label}
            </label>
          ))}
        </fieldset>
        {error ? <p role="alert" className="text-phi-sm text-destructive">{error}</p> : null}
        {saved ? <p role="status" className="text-phi-sm text-muted-foreground">{copy.networks.advancedSaved}</p> : null}
        <Button disabled={save.isPending || advanced.isLoading} onClick={() => save.mutate()}>
          {save.isPending ? copy.networks.advancedSaving : copy.networks.advancedSave}
        </Button>
      </CardBody>
    </Card>
  );
}
