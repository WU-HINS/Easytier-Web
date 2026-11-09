import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useRef, useState } from 'react';
import { useCopy } from '../../copy/locale';
import { networksApi } from './api';
import { Button, Input } from '../../ui';
import { describeError } from '../../lib/errors';

/**
 * Inline editor for the two per member fields the PATCH endpoint owns. The
 * endpoint existed but nothing in the console reached it, so changing a member's
 * hostname or address meant editing the database (root.md, known gaps).
 *
 * Only the fields the operator actually touched are sent: the server writes just
 * the named columns (F-228).
 */
export function MemberEditor({
  networkID,
  deviceID,
  hostname,
  address,
  onDone,
}: {
  networkID: string;
  deviceID: string;
  hostname?: string;
  address?: string;
  onDone: () => void;
}) {
  const copy = useCopy();
  const queryClient = useQueryClient();
  // Compare against what the editor opened with, not against the current props:
  // a background refresh during editing would otherwise make untouched fields
  // look edited and write them back (F-242).
  const initial = useRef({ hostname: hostname ?? '', address: address ?? '' });
  const [nextHostname, setNextHostname] = useState(initial.current.hostname);
  const [nextAddress, setNextAddress] = useState(initial.current.address);
  const [saved, setSaved] = useState(false);
  const dirty = nextHostname !== initial.current.hostname || nextAddress !== initial.current.address;
  const advanceBaseline = () => { initial.current = { hostname: nextHostname, address: nextAddress }; };

  const save = useMutation({
    mutationFn: () => {
      const patch: { hostname_override?: string; virtual_ipv4?: string } = {};
      if (nextHostname !== initial.current.hostname) patch.hostname_override = nextHostname;
      if (nextAddress !== initial.current.address) patch.virtual_ipv4 = nextAddress;
      if (Object.keys(patch).length === 0) return Promise.resolve();
      return networksApi.patchMember(networkID, deviceID, patch);
    },
    onSuccess: () => {
      // Keep the editor mounted so the confirmation can actually be seen and
      // announced; it used to be set and unmounted in the same tick (F-242).
      advanceBaseline();
      setSaved(true);
      void queryClient.invalidateQueries({ queryKey: ['network-members', networkID] });
    },
  });

  return (
    <div className="w-full space-y-[var(--u-3)]">
      <p className="text-phi-sm font-medium">{copy.networks.memberEditTitle}</p>
      <label className="block">
        <span className="mb-[var(--u-2)] block text-phi-sm text-muted-foreground">
          {copy.networks.memberEditHostname}
        </span>
        <Input
          value={nextHostname}
          onChange={(event) => { setSaved(false); setNextHostname(event.target.value); }}
          maxLength={64}
        />
      </label>
      <label className="block">
        <span className="mb-[var(--u-2)] block text-phi-sm text-muted-foreground">
          {copy.networks.memberEditAddress}
        </span>
        <Input
          value={nextAddress}
          onChange={(event) => { setSaved(false); setNextAddress(event.target.value); }}
          placeholder="10.0.0.2"
        />
      </label>
      {save.isError ? (
        <p role="alert" className="text-phi-sm text-destructive">
          {describeError(save.error, copy)}
        </p>
      ) : null}
      {saved ? <p role="status" className="text-phi-sm text-muted-foreground">{copy.networks.memberEditSaved}</p> : null}
      <div className="flex items-center gap-[var(--u-2)]">
        <Button disabled={save.isPending || !dirty} onClick={() => save.mutate()}>
          {copy.networks.memberEditSave}
        </Button>
        <Button variant="ghost" onClick={onDone}>
          {copy.networks.memberEditCancel}
        </Button>
      </div>
    </div>
  );
}
