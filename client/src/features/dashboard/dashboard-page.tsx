import { useQuery } from '@tanstack/react-query';
import { copy } from '../../copy/zh-CN';
import { api } from '../../lib/http';
import { Badge, Card, CardBody, CardHeader, Skeleton } from '../../ui';

export function DashboardPage() {
  const summary = useQuery({ queryKey: ['summary'], queryFn: api.summary, refetchInterval: 5_000 });
  const machines = useQuery({ queryKey: ['machines'], queryFn: api.machines, refetchInterval: 5_000 });

  const stats = [
    { label: copy.dashboard.devices, value: summary.data?.device_count },
    { label: copy.dashboard.online, value: summary.data?.online_device_count },
  ];

  const recent = (machines.data ?? []).filter((machine) => machine.online).slice(0, 5);

  return (
    <div className="space-y-[var(--gap-section)]">
      <header>
        <h1 className="text-phi-2xl font-semibold tracking-tight">{copy.dashboard.title}</h1>
      </header>

      <div className="grid gap-[var(--gap-card)] sm:grid-cols-2">
        {stats.map((stat) => (
          <Card key={stat.label}>
            <CardBody>
              <p className="text-phi-sm text-muted-foreground">{stat.label}</p>
              <p className="mt-[var(--u-2)] text-phi-3xl font-semibold tabular-nums">
                {stat.value === undefined ? <Skeleton className="h-[var(--u-10)] w-[var(--u-11)]" /> : stat.value}
              </p>
            </CardBody>
          </Card>
        ))}
      </div>

      <Card>
        <CardHeader title={copy.dashboard.recent} />
        <CardBody className="space-y-[var(--u-3)]">
          {machines.isLoading ? <Skeleton className="h-[var(--u-12)]" /> : null}
          {!machines.isLoading && recent.length === 0 ? (
            <p className="text-phi-sm text-muted-foreground">{copy.dashboard.empty}</p>
          ) : null}
          {recent.map((machine) => (
            <div key={machine.machine_id} className="flex items-center justify-between gap-[var(--u-4)] border-b border-border pb-[var(--u-3)] last:border-0 last:pb-0">
              <div className="min-w-0">
                <p className="truncate text-phi-base font-medium">{machine.alias || machine.hostname || machine.machine_id}</p>
                <p className="truncate text-phi-sm text-muted-foreground">{machine.client_url || '—'}</p>
              </div>
              <Badge tone="success">{copy.devices.online}</Badge>
            </div>
          ))}
        </CardBody>
      </Card>
    </div>
  );
}
