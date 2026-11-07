import { useQuery } from '@tanstack/react-query';
import { useCopy } from '../../copy/locale';
import { api, type InstancePeerConn } from '../../lib/http';
import { Badge, Card, CardBody, Skeleton } from '../../ui';
import { AclStatsPanel } from './acl-stats-panel';
import { LoggerLevelPanel } from './logger-level-panel';

const mb = (bytes?: number) => ((bytes ?? 0) / 1024 / 1024).toFixed(1) + ' MB';
const ms = (micro?: number) => ((micro ?? 0) / 1000).toFixed(1) + ' ms';
const pct = (rate?: number) => ((rate ?? 0) * 100).toFixed(1) + '%';

function activeConn(conns?: InstancePeerConn[]): InstancePeerConn | undefined {
  if (!conns || conns.length === 0) return undefined;
  return conns.find((conn) => !conn.isClosed) || conns[0];
}

/**
 * What one running instance reports about itself: its peers with the connection
 * they hold (tunnel type, remote address, latency, loss, traffic) and its route
 * table. The numbers come from the device, not from the console's own view.
 */
export function InstanceDetailPanel({ machineID, instanceID }: { machineID: string; instanceID: string }) {
  const copy = useCopy();
  const info = useQuery({
    queryKey: ['instance-info', machineID, instanceID],
    queryFn: () => api.instanceInfo(machineID, instanceID),
    refetchInterval: 5000,
  });

  if (info.isLoading) return <Skeleton className="h-[var(--u-24)]" />;
  if (info.isError) {
    return (
      <Card>
        <CardBody className="space-y-[var(--u-3)]">
          <h3 className="text-phi-base font-semibold">{copy.networks.instanceDetail}</h3>
          <p role="alert" className="text-phi-sm text-destructive">
            {(info.error as Error).message}
          </p>
        </CardBody>
      </Card>
    );
  }

  const running = info.data && info.data.info ? info.data.info[instanceID] : undefined;
  const peers = running && running.peers ? running.peers : [];
  const routes = running && running.routes ? running.routes : [];

  return (
    <Card>
      <CardBody className="space-y-[var(--u-4)]">
        <div className="flex flex-wrap items-center justify-between gap-[var(--u-3)]">
          <h3 className="text-phi-base font-semibold">{copy.networks.instanceDetail}</h3>
          <Badge tone={running && running.running ? 'success' : 'neutral'}>
            {running && running.running ? copy.networks.running : copy.networks.instanceStopped}
          </Badge>
        </div>
        {running && running.errorMsg ? (
          <p role="alert" className="text-phi-sm text-destructive">{running.errorMsg}</p>
        ) : null}

        <div>
          <h4 className="text-phi-sm font-medium text-muted-foreground">{copy.networks.peersTitle}</h4>
          {peers.length === 0 ? (
            <p className="mt-[var(--u-2)] text-phi-sm text-muted-foreground">{copy.networks.noPeers}</p>
          ) : (
            <div className="mt-[var(--u-2)] overflow-x-auto">
              <table className="w-full border-collapse text-left text-phi-sm">
                <thead>
                  <tr className="border-b border-border text-muted-foreground">
                    <th className="py-[var(--u-2)] pr-[var(--u-3)] font-medium">{copy.networks.peerId}</th>
                    <th className="py-[var(--u-2)] pr-[var(--u-3)] font-medium">{copy.networks.tunnel}</th>
                    <th className="py-[var(--u-2)] pr-[var(--u-3)] font-medium">{copy.networks.remote}</th>
                    <th className="py-[var(--u-2)] pr-[var(--u-3)] font-medium">{copy.networks.latency}</th>
                    <th className="py-[var(--u-2)] pr-[var(--u-3)] font-medium">{copy.networks.loss}</th>
                    <th className="py-[var(--u-2)] pr-[var(--u-3)] font-medium">{copy.networks.traffic}</th>
                  </tr>
                </thead>
                <tbody>
                  {peers.map((peer) => {
                    const conn = activeConn(peer.conns);
                    const tunnel = conn && conn.tunnel ? conn.tunnel : undefined;
                    const stats = conn && conn.stats ? conn.stats : undefined;
                    return (
                      <tr key={String(peer.peerId)} className="border-b border-border last:border-0">
                        <td className="py-[var(--u-2)] pr-[var(--u-3)] tabular-nums">{peer.peerId}</td>
                        <td className="py-[var(--u-2)] pr-[var(--u-3)]">{tunnel && tunnel.tunnelType ? tunnel.tunnelType : '—'}</td>
                        <td className="py-[var(--u-2)] pr-[var(--u-3)] font-mono text-xs">
                          {tunnel && tunnel.remoteAddr && tunnel.remoteAddr.host ? tunnel.remoteAddr.host : '—'}
                        </td>
                        <td className="py-[var(--u-2)] pr-[var(--u-3)] tabular-nums">{ms(stats && stats.latencyUs)}</td>
                        <td className="py-[var(--u-2)] pr-[var(--u-3)] tabular-nums">{pct(conn && conn.lossRate)}</td>
                        <td className="py-[var(--u-2)] pr-[var(--u-3)] tabular-nums">
                          {mb(stats && stats.rxBytes)} / {mb(stats && stats.txBytes)}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </div>

        <div>
          <h4 className="text-phi-sm font-medium text-muted-foreground">{copy.networks.routesTitle}</h4>
          {routes.length === 0 ? (
            <p className="mt-[var(--u-2)] text-phi-sm text-muted-foreground">{copy.networks.noRoutes}</p>
          ) : (
            <div className="mt-[var(--u-2)] overflow-x-auto">
              <table className="w-full border-collapse text-left text-phi-sm">
                <thead>
                  <tr className="border-b border-border text-muted-foreground">
                    <th className="py-[var(--u-2)] pr-[var(--u-3)] font-medium">{copy.networks.peerId}</th>
                    <th className="py-[var(--u-2)] pr-[var(--u-3)] font-medium">{copy.networks.routeHostname}</th>
                    <th className="py-[var(--u-2)] pr-[var(--u-3)] font-medium">{copy.networks.routeCost}</th>
                    <th className="py-[var(--u-2)] pr-[var(--u-3)] font-medium">{copy.networks.nextHop}</th>
                  </tr>
                </thead>
                <tbody>
                  {routes.map((route) => (
                    <tr key={String(route.peerId) + '-' + String(route.nextHopPeerId)} className="border-b border-border last:border-0">
                      <td className="py-[var(--u-2)] pr-[var(--u-3)] tabular-nums">{route.peerId}</td>
                      <td className="py-[var(--u-2)] pr-[var(--u-3)]">{route.hostname || '—'}</td>
                      <td className="py-[var(--u-2)] pr-[var(--u-3)] tabular-nums">{route.cost ?? '—'}</td>
                      <td className="py-[var(--u-2)] pr-[var(--u-3)] tabular-nums">{route.nextHopPeerId ?? '—'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
        <LoggerLevelPanel machineID={machineID} />
        <AclStatsPanel machineID={machineID} instanceID={instanceID} />
      </CardBody>
    </Card>
  );
}
