import { useQueries, useQuery } from '@tanstack/react-query';
import { Link, useParams } from 'react-router-dom';
import { useCopy } from '../../copy/locale';
import { api } from '../../lib/http';
import { Badge, Card, CardBody, Skeleton } from '../../ui';
import { DeviceActions } from './device-actions';
import { describeError } from '../../lib/errors';
import { useMachines } from './use-devices';

function formatTime(value: string): string {
  if (!value) return '—';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString('zh-CN', { hour12: false });
}

export function DeviceDetailPage() {
  const copy = useCopy();
  const params = useParams();
  const machineID = params.machineID || '';
  const machines = useMachines(5000);
  const networks = useQuery({ queryKey: ['networks'], queryFn: api.networks });
  const networkRows = networks.data || [];
  const memberQueries = useQueries({
    queries: networkRows.map((network) => ({
      queryKey: ['members', network.network_id],
      queryFn: () => api.networkMembers(network.network_id),
    })),
  });
  const device = (machines.data || []).find((row) => row.machine_id === machineID);

  if (machines.isLoading) {
    return (
      <div className="space-y-[var(--u-4)]">
        <Skeleton className="h-[var(--u-12)]" />
        <Skeleton className="h-[var(--u-24)]" />
      </div>
    );
  }

  if (machines.isError) {
    return (
      <Card>
        <CardBody className="space-y-[var(--u-4)]">
          <p role="alert" className="text-phi-sm text-destructive">
            {describeError(machines.error, copy)}
          </p>
          <Link className="text-phi-sm underline underline-offset-4" to="/devices">
            {copy.devices.detailBack}
          </Link>
        </CardBody>
      </Card>
    );
  }

  if (!device) {
    return (
      <Card>
        <CardBody className="space-y-[var(--u-4)]">
          <p className="text-phi-sm text-muted-foreground">{copy.devices.detailMissing}</p>
          <Link className="text-phi-sm underline underline-offset-4" to="/devices">
            {copy.devices.detailBack}
          </Link>
        </CardBody>
      </Card>
    );
  }

  const memberships = networkRows
    .map((network, index) => {
      const rows = memberQueries[index].data ? memberQueries[index].data.members : [];
      const member = rows.find((row) => row.device_id === machineID);
      return { network, member };
    })
    .filter((entry) => entry.member !== undefined);

  return (
    <div className="space-y-[var(--gap-section)]">
      <header className="space-y-[var(--u-3)]">
        <Link className="text-phi-sm text-muted-foreground underline underline-offset-4" to="/devices">
          {copy.devices.detailBack}
        </Link>
        <div className="flex flex-wrap items-end justify-between gap-[var(--gap-card)]">
          <div className="min-w-0">
            <h1 className="truncate text-phi-3xl font-semibold tracking-tight">
              {device.alias || device.hostname || device.machine_id}
            </h1>
            <p className="mt-[var(--u-2)] break-all text-phi-sm text-muted-foreground">{device.machine_id}</p>
          </div>
          <div className="flex shrink-0 items-center gap-[var(--u-2)]">
            <Badge tone={device.online ? 'success' : 'neutral'}>
              {device.online ? copy.devices.online : copy.devices.offline}
            </Badge>
            <DeviceActions machine={device} />
          </div>
        </div>
        <dl className="grid gap-[var(--u-3)] text-phi-sm sm:grid-cols-3">
          <div>
            <dt className="text-muted-foreground">{copy.devices.columns.version}</dt>
            <dd className="mt-[var(--u-1)]">{device.easytier_version || '—'}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground">{copy.devices.columns.address}</dt>
            <dd className="mt-[var(--u-1)] break-all">{device.client_url || '—'}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground">{copy.devices.columns.lastSeen}</dt>
            <dd className="mt-[var(--u-1)]">{formatTime(device.last_seen)}</dd>
          </div>
        </dl>
      </header>
      <Card>
        <CardBody className="space-y-[var(--u-3)]">
          <h2 className="text-phi-xl font-semibold">{copy.devices.detailNetworks}</h2>
          {memberships.length === 0 ? (
            <p className="text-phi-sm text-muted-foreground">{copy.devices.detailNoNetworks}</p>
          ) : null}
          {memberships.map((entry) => (
            <div
              key={entry.network.network_id}
              className="flex flex-wrap items-center justify-between gap-[var(--u-3)] border-b border-border pb-[var(--u-2)] last:border-0"
            >
              <div className="min-w-0">
                <p className="truncate font-medium">{entry.network.display_name}</p>
                <p className="truncate text-phi-sm text-muted-foreground">{entry.network.network_name}</p>
              </div>
              <div className="flex shrink-0 items-center gap-[var(--u-2)]">
                <span className="font-mono text-phi-sm">{entry.member ? entry.member.virtual_ipv4 : '—'}</span>
                {entry.member && entry.member.running ? <Badge tone="success">{copy.networks.running}</Badge> : null}
                <Link className="text-phi-sm underline underline-offset-4" to={'/networks/' + entry.network.network_id}>
                  {copy.devices.detailOpen}
                </Link>
              </div>
            </div>
          ))}
        </CardBody>
      </Card>
    </div>
  );
}
