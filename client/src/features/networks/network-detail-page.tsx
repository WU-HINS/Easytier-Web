import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { copy } from '../../copy/zh-CN';
import { api } from '../../lib/http';
import { Badge, Card, CardBody, Skeleton } from '../../ui';
import { NetworkMembers } from './network-members';
import { NetworkSettings } from './network-settings';

/**
 * One network as its own page, the way the first console organised it: the list
 * is for finding a network, everything about it lives here. Keeping the detail
 * on a route also means a network can be linked to and reloaded.
 */
export function NetworkDetailPage() {
  const { networkID = '' } = useParams();
  const [tab, setTab] = useState<'members' | 'settings'>('members');
  // The header needs the subnet, which only the single-network endpoint returns,
  // so the page reads that and keeps the list around for the member count.
  const detail = useQuery({ queryKey: ['network', networkID], queryFn: () => api.network(networkID), enabled: networkID !== '' });
  const networks = useQuery({ queryKey: ['networks'], queryFn: api.networks });
  const network = detail.data;
  const memberCount = (networks.data ?? []).find((row) => row.network_id === networkID)?.member_count;

  if (detail.isLoading) {
    return (
      <div className="space-y-[var(--u-4)]">
        <Skeleton className="h-[var(--u-12)]" />
        <Skeleton className="h-[var(--u-24)]" />
      </div>
    );
  }

  if (detail.isError) {
    return (
      <Card>
        <CardBody className="space-y-[var(--u-4)]">
          <p role="alert" className="text-phi-sm text-destructive">
            {(detail.error as Error).message}
          </p>
          <Link className="text-phi-sm underline underline-offset-4" to="/networks">
            {copy.networks.detailBack}
          </Link>
        </CardBody>
      </Card>
    );
  }

  if (!network) {
    return (
      <Card>
        <CardBody className="space-y-[var(--u-4)]">
          <p className="text-phi-sm text-muted-foreground">{copy.networks.detailMissing}</p>
          <Link className="text-phi-sm underline underline-offset-4" to="/networks">
            {copy.networks.detailBack}
          </Link>
        </CardBody>
      </Card>
    );
  }

  const tabs = [
    { key: 'members' as const, label: copy.networks.detailTabMembers },
    { key: 'settings' as const, label: copy.networks.detailTabSettings },
  ];

  return (
    <div className="space-y-[var(--gap-section)]">
      <header className="space-y-[var(--u-3)]">
        <Link className="text-phi-sm text-muted-foreground underline underline-offset-4" to="/networks">
          {copy.networks.detailBack}
        </Link>
        <div className="flex flex-wrap items-end justify-between gap-[var(--gap-card)]">
          <div className="min-w-0">
            <h1 className="truncate text-phi-2xl font-semibold tracking-tight">{network.display_name}</h1>
            <p className="mt-[var(--u-2)] truncate text-phi-sm text-muted-foreground">
              {network.network_name} · {network.networking_method} · {network.virtual_cidr ?? '—'}
            </p>
          </div>
          <div className="flex shrink-0 items-center gap-[var(--u-2)]">
            {network.secure_mode ? <Badge tone="primary">{copy.networks.secure}</Badge> : null}
            <Badge>
              {memberCount ?? 0} {copy.networks.members}
            </Badge>
          </div>
        </div>
      </header>

      <div role="tablist" aria-label={network.display_name} className="flex gap-[var(--u-2)] border-b border-border">
        {tabs.map((entry) => (
          <button
            key={entry.key}
            type="button"
            role="tab"
            aria-selected={tab === entry.key}
            onClick={() => setTab(entry.key)}
            className={
              tab === entry.key
                ? 'border-b-2 border-primary px-[var(--u-4)] py-[var(--u-3)] text-phi-sm font-medium'
                : 'border-b-2 border-transparent px-[var(--u-4)] py-[var(--u-3)] text-phi-sm text-muted-foreground'
            }
          >
            {entry.label}
          </button>
        ))}
      </div>

      {tab === 'members' ? <NetworkMembers networkID={network.network_id} onClose={() => setTab('settings')} /> : null}
      {tab === 'settings' ? <NetworkSettings networkID={network.network_id} displayName={network.display_name} /> : null}
    </div>
  );
}
