import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { copy } from '../../copy/zh-CN';
import { api } from '../../lib/http';
import { Button, Card, CardBody } from '../../ui';

/**
 * Devices that were deleted with the "block" flag. The block list used to be a
 * one way door: nothing listed it and nothing could lift it.
 */
export function BlockedDevices() {
  const queryClient = useQueryClient();
  const blocked = useQuery({ queryKey: ['blocked-machines'], queryFn: api.blockedMachines });
  const unblock = useMutation({
    mutationFn: (machineID: string) => api.unblockMachine(machineID),
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
          <h2 className="text-phi-lg font-semibold">{copy.devices.blockedTitle}</h2>
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
          <p role="alert" className="text-phi-sm text-destructive">{(unblock.error as Error).message}</p>
        ) : null}
      </CardBody>
    </Card>
  );
}
