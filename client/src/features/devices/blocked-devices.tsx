import { useQueryClient } from '@tanstack/react-query';
import { useCopy } from '../../copy/locale';
import { Button, Card, CardBody } from '../../ui';
import { describeError } from '../../lib/errors';
import { useBlockedMachines, useUnblockMachine } from './use-devices';

/**
 * Devices that were deleted with the "block" flag. The block list used to be a
 * one way door: nothing listed it and nothing could lift it.
 */
export function BlockedDevices() {
  const copy = useCopy();
  const queryClient = useQueryClient();
  const blocked = useBlockedMachines();
  const unblock = useUnblockMachine({
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['blocked-machines'] });
      void queryClient.invalidateQueries({ queryKey: ['machines'] });
    },
  });

  const rows = blocked.data ?? [];
  if (blocked.isLoading || rows.length === 0) return null;

  return (
    <Card>
      <CardBody className="space-y-[var(--u-3)]">
        <div>
          <h2 className="text-phi-xl font-semibold">{copy.devices.blockedTitle}</h2>
          <p className="mt-[var(--u-1)] text-phi-sm text-muted-foreground">{copy.devices.blockedHint}</p>
        </div>
        {rows.map((machine) => (
          <div
            key={machine.machine_id}
            className="flex flex-wrap items-center justify-between gap-[var(--u-3)] border-b border-border pb-[var(--u-3)] last:border-0 last:pb-0"
          >
            <div className="min-w-0">
              <p className="truncate text-phi-base font-medium">{machine.hostname || machine.machine_id}</p>
              <p className="truncate text-phi-sm text-muted-foreground">
                {machine.machine_id} · {machine.attempt_count}
              </p>
            </div>
            <Button
              type="button"
              variant="ghost"
              disabled={unblock.isPending}
              onClick={() => unblock.mutate(machine.machine_id)}
            >
              {copy.devices.unblock}
            </Button>
          </div>
        ))}
        {unblock.isError ? (
          <p role="alert" className="text-phi-sm text-destructive">{describeError(unblock.error, copy)}</p>
        ) : null}
      </CardBody>
    </Card>
  );
}
