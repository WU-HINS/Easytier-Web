import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { copy } from '../../copy/zh-CN';
import { api } from '../../lib/http';
import { Skeleton } from '../../ui';

const LEVELS = ['disabled', 'error', 'warning', 'info', 'debug', 'trace'];

/**
 * The device's log level. It is per process, not per instance, so the label says
 * so; the console does not have a per-instance switch to offer.
 */
export function LoggerLevelPanel({ machineID }: { machineID: string }) {
  const queryClient = useQueryClient();
  const level = useQuery({ queryKey: ['logger-level', machineID], queryFn: () => api.loggerLevel(machineID) });
  const save = useMutation({
    mutationFn: (next: string) => api.setLoggerLevel(machineID, next),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['logger-level', machineID] });
    },
  });

  if (level.isLoading) return <Skeleton className="h-[var(--u-10)]" />;
  if (level.isError) {
    return (
      <p role="alert" className="text-phi-sm text-destructive">
        {(level.error as Error).message}
      </p>
    );
  }

  const current = level.data ? level.data.level : 'disabled';

  return (
    <div className="space-y-[var(--u-2)]">
      <div className="flex flex-wrap items-center gap-[var(--u-3)]">
        <h4 className="text-phi-sm font-medium text-muted-foreground">{copy.networks.loggerTitle}</h4>
        <select
          aria-label={copy.networks.loggerTitle}
          value={current}
          onChange={(event) => save.mutate(event.target.value)}
          className="rounded-[var(--radius)] border border-input bg-card p-[var(--u-1)] font-mono text-phi-sm"
        >
          {LEVELS.map((entry) => (
            <option key={entry} value={entry}>
              {entry}
            </option>
          ))}
        </select>
        {save.isError ? <span role="alert" className="text-phi-sm text-destructive">{(save.error as Error).message}</span> : null}
        {save.isSuccess ? <span className="text-phi-sm text-muted-foreground">{copy.networks.loggerSaved}</span> : null}
      </div>
      <p className="text-phi-sm text-muted-foreground">{copy.networks.loggerHint}</p>
    </div>
  );
}
