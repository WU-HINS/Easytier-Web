import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { useCopy } from '../../copy/locale';
import { api, formatIpv4, type Machine, type RouteEntry } from '../../lib/http';
import { Badge, Button, Card, CardBody, Skeleton } from '../../ui';

/**
 * Route table of one network instance on one device. The data comes from the
 * device over the proxied management RPC, so it only exists while the device is
 * online and its instance is running.
 */
export function DeviceRoutes({ machine, onClose }: { machine: Machine; onClose: () => void }) {
  const copy = useCopy();
  const instances = machine.running_network_instances ?? [];
  const [instance, setInstance] = useState(instances[0] ?? '');

  const capabilities = useQuery({ queryKey: ['rpc-capabilities'], queryFn: api.rpcCapabilities, staleTime: 60_000 });
  const listRouteAvailable = Boolean(
    capabilities.data?.services
      .find((service) => service.name === 'api.instance.PeerManageRpc')
      ?.methods.find((method) => method.name === 'ListRoute')?.available,
  );

  const routes = useQuery({
    queryKey: ['routes', machine.machine_id, instance],
    queryFn: () => api.machineRoutes(machine.machine_id, instance),
    enabled: Boolean(instance) && listRouteAvailable && machine.online,
    retry: false,
  });

  const rows: RouteEntry[] = routes.data?.routes ?? [];

  return (
    <Card>
      <CardBody>
        <div className="flex flex-wrap items-center justify-between gap-[var(--u-3)]">
          <h2 className="text-phi-lg font-semibold">{copy.routes.title}</h2>
          <div className="flex items-center gap-[var(--u-2)]">
            <Badge tone={machine.online ? 'success' : 'neutral'}>
              {machine.online ? copy.devices.online : copy.devices.offline}
            </Badge>
            <Button variant="ghost" onClick={onClose}>
              {copy.networks.back}
            </Button>
          </div>
        </div>

        {!listRouteAvailable && capabilities.isSuccess ? (
          <p className="mt-[var(--u-4)] text-phi-sm text-muted-foreground">{copy.routes.unavailable}</p>
        ) : null}

        {instances.length === 0 ? (
          <p className="mt-[var(--u-4)] text-phi-sm text-muted-foreground">{copy.routes.noInstance}</p>
        ) : null}

        {instances.length > 0 ? (
          <label className="mt-[var(--u-4)] block">
            <span className="mb-[var(--u-2)] block text-phi-sm text-muted-foreground">{copy.routes.instance}</span>
            <select
              value={instance}
              onChange={(event) => setInstance(event.target.value)}
              className="h-[var(--u-10)] w-full rounded-[var(--radius)] border border-input bg-card px-[var(--u-3)] text-phi-base text-foreground"
            >
              {instances.map((value) => (
                <option key={value} value={value}>
                  {value}
                </option>
              ))}
            </select>
          </label>
        ) : null}

        <div className="mt-[var(--u-5)]">
          {routes.isLoading ? <Skeleton className="h-[var(--u-10)]" /> : null}
          {routes.isError ? (
            <p role="alert" className="text-phi-sm text-destructive">{(routes.error as Error).message}</p>
          ) : null}
          {!routes.isLoading && !routes.isError && rows.length === 0 && instance ? (
            <p className="text-phi-sm text-muted-foreground">{copy.routes.empty}</p>
          ) : null}
          {rows.length > 0 ? (
            <table className="w-full border-collapse text-left text-phi-base">
              <thead>
                <tr className="border-b border-border text-phi-sm text-muted-foreground">
                  <th className="py-[var(--u-3)] font-medium">{copy.routes.peer}</th>
                  <th className="py-[var(--u-3)] font-medium">{copy.routes.hostname}</th>
                  <th className="py-[var(--u-3)] font-medium">{copy.routes.address}</th>
                  <th className="py-[var(--u-3)] text-right font-medium">{copy.routes.cost}</th>
                  <th className="py-[var(--u-3)] text-right font-medium">{copy.routes.latency}</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((route) => (
                  <tr key={String(route.peer_id)} className="border-b border-border last:border-0">
                    <td className="py-[var(--u-3)] tabular-nums">{route.peer_id ?? '—'}</td>
                    <td className="py-[var(--u-3)]">{route.hostname || '—'}</td>
                    <td className="py-[var(--u-3)] tabular-nums text-muted-foreground">
                      {formatIpv4(route.ipv4_addr?.address?.addr, route.ipv4_addr?.network_length)}
                    </td>
                    <td className="py-[var(--u-3)] text-right tabular-nums">{route.cost ?? '—'}</td>
                    <td className="py-[var(--u-3)] text-right tabular-nums text-muted-foreground">
                      {route.path_latency === undefined ? '—' : String(route.path_latency) + ' ms'}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : null}
        </div>
      </CardBody>
    </Card>
  );
}
