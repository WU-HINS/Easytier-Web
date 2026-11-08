import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { Link } from 'react-router-dom';
import { useCopy } from '../../copy/locale';
import { api } from '../../lib/http';
import { Badge, Button, Card, CardBody, CardHeader, Skeleton } from '../../ui';

export function DashboardPage() {
  const copy = useCopy();
  // The original console could pause the polling; an operator watching a quiet
  // cluster should not be paying for a request every five seconds.
  const [autoRefresh, setAutoRefresh] = useState(true);
  const interval = autoRefresh ? 5000 : false;
  const summary = useQuery({ queryKey: ['summary'], queryFn: api.summary, refetchInterval: interval });
  const machines = useQuery({ queryKey: ['machines'], queryFn: api.machines, refetchInterval: interval });
  const networks = useQuery({ queryKey: ['networks'], queryFn: api.networks, refetchInterval: interval });

  const devices = machines.data ?? [];
  const networkRows = networks.data ?? [];
  const instanceCount = devices.reduce((total, machine) => total + (machine.running_network_instances || []).length, 0);

  // A failed request must not render as zero: "0 networks" when the API is down is
  // the interface telling the operator they have nothing (review team, r259).
  const failed = summary.isError || machines.isError || networks.isError;
  const retry = () => {
    void summary.refetch();
    void machines.refetch();
    void networks.refetch();
  };
  const stats = [
    { label: copy.dashboard.devices, value: summary.isError ? undefined : summary.data?.device_count, failed: summary.isError },
    { label: copy.dashboard.online, value: summary.isError ? undefined : summary.data?.online_device_count, failed: summary.isError },
    { label: copy.dashboard.networks, value: networks.isError || networks.isLoading ? undefined : networkRows.length, failed: networks.isError },
    { label: copy.dashboard.instances, value: machines.isError || machines.isLoading ? undefined : instanceCount, failed: machines.isError },
  ];

  const recentDevices = devices.filter((machine) => machine.online).slice(0, 5);
  const recentNetworks = networkRows.slice().sort((a, b) => a.display_name.localeCompare(b.display_name)).slice(0, 5);

  return (
    <div className="space-y-[var(--gap-section)]">
      <header className="flex flex-wrap items-end justify-between gap-[var(--gap-card)]">
        <h1 className="text-phi-2xl font-semibold tracking-tight">{copy.dashboard.title}</h1>
        <label className="flex items-center gap-[var(--u-2)] text-phi-sm text-muted-foreground">
          <input type='checkbox' checked={autoRefresh} onChange={(event) => setAutoRefresh(event.target.checked)} />
          {copy.dashboard.autoRefresh}
        </label>
      </header>

      {failed ? (
        <div role="alert" className="flex flex-wrap items-center justify-between gap-[var(--u-4)] rounded-[calc(var(--radius)*0.8)] border border-destructive/40 bg-destructive/5 px-[var(--u-6)] py-[var(--u-4)] text-phi-sm text-destructive">
          <span>{copy.dashboard.loadFailed}</span>
          <Button variant="secondary" size="sm" onClick={retry}>{copy.common.retry}</Button>
        </div>
      ) : null}

      <div className="grid gap-[var(--gap-card)] sm:grid-cols-2 lg:grid-cols-4">
        {stats.map((stat) => (
          <Card key={stat.label}>
            <CardBody>
              <p className="text-phi-sm text-muted-foreground">{stat.label}</p>
              <p className="mt-[var(--u-2)] text-phi-3xl font-semibold tabular-nums">
                {stat.failed ? (
                  <span className="text-muted-foreground">—</span>
                ) : stat.value === undefined ? (
                  <Skeleton className="h-[var(--u-10)] w-[var(--u-11)]" />
                ) : (
                  stat.value
                )}
              </p>
            </CardBody>
          </Card>
        ))}
      </div>

      <div className="grid gap-[var(--gap-card)] lg:grid-cols-2">
        <Card>
          <CardHeader title={copy.dashboard.recent} />
          <CardBody className="space-y-[var(--u-3)]">
            {machines.isLoading ? <Skeleton className="h-[var(--u-12)]" /> : null}
            {!machines.isLoading && !machines.isError && recentDevices.length === 0 ? (
              <p className="text-phi-sm text-muted-foreground">{copy.dashboard.empty}</p>
            ) : null}
            {recentDevices.map((machine) => (
              <div key={machine.machine_id} className="flex items-center justify-between gap-[var(--u-4)] border-b border-border pb-[var(--u-3)] last:border-0 last:pb-0">
                <div className="min-w-0">
                  <p className="truncate text-phi-base font-medium">{machine.alias || machine.hostname || machine.machine_id}</p>
                  <p className="truncate text-phi-sm text-muted-foreground">{machine.client_url || '—'}</p>
                </div>
                <Badge tone='success'>{copy.devices.online}</Badge>
              </div>
            ))}
            <Link className="inline-flex items-center min-h-[var(--u-11)] text-phi-sm underline underline-offset-4" to='/devices'>
              {copy.dashboard.viewAll}
            </Link>
          </CardBody>
        </Card>

        <Card>
          <CardHeader title={copy.dashboard.networks} />
          <CardBody className="space-y-[var(--u-3)]">
            {networks.isLoading ? <Skeleton className="h-[var(--u-12)]" /> : null}
            {!networks.isLoading && !networks.isError && recentNetworks.length === 0 ? (
              <p className="text-phi-sm text-muted-foreground">{copy.dashboard.noNetworks}</p>
            ) : null}
            {recentNetworks.map((network) => (
              <div key={network.network_id} className="flex items-center justify-between gap-[var(--u-4)] border-b border-border pb-[var(--u-3)] last:border-0 last:pb-0">
                <div className="min-w-0">
                  <p className="truncate text-phi-base font-medium">{network.display_name}</p>
                  <p className="truncate text-phi-sm text-muted-foreground">{network.networking_method}</p>
                </div>
                <Badge>
                  {network.member_count} {copy.networks.members}
                </Badge>
              </div>
            ))}
            <Link className="inline-flex items-center min-h-[var(--u-11)] text-phi-sm underline underline-offset-4" to='/networks'>
              {copy.dashboard.viewAll}
            </Link>
          </CardBody>
        </Card>
      </div>
    </div>
  );
}
