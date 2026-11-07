import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { useCopy } from '../../copy/locale';
import { api } from '../../lib/http';
import { Badge, Card, CardBody, Skeleton } from '../../ui';
import { NetworkMembers } from './network-members';
import { NetworkAclPanel } from './network-acl-panel';
import { NetworkCredentialsPanel } from './network-credentials-panel';
import { NetworkSettings } from './network-settings';
import { describeError } from '../../lib/errors';

/**
 * One network as its own page, the way the first console organised it: the list
 * is for finding a network, everything about it lives here. Keeping the detail
 * on a route also means a network can be linked to and reloaded.
 */
export function NetworkDetailPage() {
  const copy = useCopy();
  const { networkID = '' } = useParams();
  const [tab, setTab] = useState<'members' | 'acl' | 'credentials' | 'settings'>('members');
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
            {describeError(detail.error, copy)}
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
    { key: 'acl' as const, label: copy.networks.detailTabAcl },
    { key: 'credentials' as const, label: copy.networks.detailTabCredentials },
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
            id={"tab-" + entry.key}
            aria-selected={tab === entry.key}
            aria-controls={"panel-" + entry.key}
            tabIndex={tab === entry.key ? 0 : -1}
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

      {/* A panel per tab so a screen reader can follow the tab to its content. */}
      <div role="tabpanel" id="panel-members" aria-labelledby="tab-members" hidden={tab !== 'members'}>
        {tab === 'members' ? <NetworkMembers networkID={network.network_id} onClose={() => setTab('settings')} /> : null}
      </div>
      <div role="tabpanel" id="panel-acl" aria-labelledby="tab-acl" hidden={tab !== 'acl'}>
        {tab === 'acl' ? <NetworkAclPanel networkID={network.network_id} /> : null}
      </div>
      <div role="tabpanel" id="panel-credentials" aria-labelledby="tab-credentials" hidden={tab !== 'credentials'}>
        {tab === 'credentials' ? <NetworkCredentialsPanel networkID={network.network_id} /> : null}
      </div>
      <div role="tabpanel" id="panel-settings" aria-labelledby="tab-settings" hidden={tab !== 'settings'}>
        {tab === 'settings' ? <NetworkSettings networkID={network.network_id} displayName={network.display_name} /> : null}
      </div>
    </div>
  );
}
