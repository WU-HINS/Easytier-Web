import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { useCopy } from '../../copy/locale';
import { api, type Machine } from '../../lib/http';
import { Button, Input } from '../../ui';

/**
 * Rename and delete for one device. Deleting also drops the device from every
 * network on the server side, so the confirmation says so explicitly.
 */
export function DeviceActions({ machine }: { machine: Machine }) {
  const copy = useCopy();
  const queryClient = useQueryClient();
  const [editing, setEditing] = useState(false);
  const [alias, setAlias] = useState(machine.alias ?? '');

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['machines'] });
    void queryClient.invalidateQueries({ queryKey: ['networks'] });
  };

  const rename = useMutation({
    mutationFn: (value: string) => api.renameMachine(machine.machine_id, value),
    onSuccess: () => {
      setEditing(false);
      refresh();
    },
  });
  const remove = useMutation({
    mutationFn: () => api.deleteMachine(machine.machine_id),
    onSuccess: refresh,
  });

  if (editing) {
    return (
      <div className="flex flex-wrap items-center justify-end gap-[var(--u-2)]">
        <Input
          aria-label={copy.devices.rename}
          value={alias}
          placeholder={machine.hostname || machine.machine_id}
          onChange={(event) => setAlias(event.target.value)}
          className="w-full max-w-[var(--u-16)]"
        />
        <Button
          type="button"
          disabled={rename.isPending}
          onClick={() => rename.mutate(alias.trim())}
        >
          {copy.devices.save}
        </Button>
        <Button type="button" variant="ghost" onClick={() => setEditing(false)}>
          {copy.devices.cancel}
        </Button>
        {rename.isError ? (
          <span className="text-phi-sm text-destructive">{(rename.error as Error).message}</span>
        ) : null}
      </div>
    );
  }

  return (
    <div className="flex flex-wrap items-center justify-end gap-[var(--u-2)]">
      <Button type="button" variant="ghost" onClick={() => setEditing(true)}>
        {copy.devices.rename}
      </Button>
      <Button
        type="button"
        variant="ghost"
        disabled={remove.isPending}
        onClick={() => {
          if (window.confirm(copy.devices.confirmDelete)) remove.mutate();
        }}
      >
        {copy.devices.delete}
      </Button>
      {remove.isError ? (
        <span className="text-phi-sm text-destructive">{(remove.error as Error).message}</span>
      ) : null}
    </div>
  );
}
