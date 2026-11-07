import { useQuery } from '@tanstack/react-query';
import { useCopy } from '../../copy/locale';
import { api } from '../../lib/http';
import { Card, CardBody, Skeleton } from '../../ui';

/**
 * ACL counters for one running instance. The numbers live on the device, so this
 * panel is the only place in the console that reads past the config plane.
 */
export function AclStatsPanel({ machineID, instanceID }: { machineID: string; instanceID: string }) {
  const copy = useCopy();
  const stats = useQuery({
    queryKey: ['acl-stats', machineID, instanceID],
    queryFn: () => api.aclStats(machineID, instanceID),
  });

  if (stats.isLoading) return <Skeleton className="h-[var(--u-10)]" />;
  if (stats.isError) {
    return (
      <Card>
        <CardBody className="space-y-[var(--u-3)]">
          <h3 className="text-phi-base font-semibold">{copy.networks.aclStats}</h3>
          <p role="alert" className="text-phi-sm text-destructive">
            {(stats.error as Error).message}
          </p>
        </CardBody>
      </Card>
    );
  }

  const rules = stats.data?.rules ?? [];
  return (
    <Card>
      <CardBody className="space-y-[var(--u-3)]">
        <h3 className="text-phi-base font-semibold">{copy.networks.aclStats}</h3>
        {rules.length === 0 ? (
          <p className="text-phi-sm text-muted-foreground">{copy.networks.aclStatsEmpty}</p>
        ) : (
          <table className="w-full border-collapse text-left text-phi-sm">
            <thead>
              <tr className="border-b border-border text-muted-foreground">
                <th className="py-[var(--u-2)] font-medium">{copy.networks.aclRule}</th>
                <th className="py-[var(--u-2)] font-medium">{copy.networks.aclPackets}</th>
                <th className="py-[var(--u-2)] font-medium">{copy.networks.aclBytes}</th>
              </tr>
            </thead>
            <tbody>
              {rules.map((entry, index) => (
                <tr key={(entry.rule && entry.rule.id) || String(index)} className="border-b border-border last:border-0">
                  <td className="py-[var(--u-2)]">{(entry.rule && entry.rule.name) || '—'}</td>
                  <td className="py-[var(--u-2)] tabular-nums">{entry.stat?.packet_count ?? 0}</td>
                  <td className="py-[var(--u-2)] tabular-nums">{entry.stat?.byte_count ?? 0}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </CardBody>
    </Card>
  );
}
