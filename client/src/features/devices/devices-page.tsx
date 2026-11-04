import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { Link } from 'react-router-dom';
import { copy } from '../../copy/zh-CN';
import { api, type Machine } from '../../lib/http';
import { Badge, Card, CardBody, Input, Skeleton } from '../../ui';
import { EnrollCard } from './enroll-card';
import { DeviceActions } from './device-actions';
import { BlockedDevices } from './blocked-devices';
import { DeviceRoutes } from './device-routes';

function formatTime(value: string): string {
  if (!value) return '—';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString('zh-CN', { hour12: false });
}

function displayName(machine: Machine): string {
  return machine.alias || machine.hostname || machine.machine_id;
}

export function DevicesPage() {
  const machines = useQuery({ queryKey: ['machines'], queryFn: api.machines, refetchInterval: 5_000 });
  const [query, setQuery] = useState('');
  const rows = machines.data ?? [];
  // Search by name, alias or machine id, the way the original device list does.
  const needle = query.trim().toLocaleLowerCase();
  const visible = needle
    ? rows.filter((machine) => {
        const haystack = [machine.hostname, machine.alias, machine.machine_id].join(" ").toLocaleLowerCase();
        return haystack.includes(needle);
      })
    : rows;
  const [selected, setSelected] = useState<string | null>(null);

  return (
    <div className="space-y-[var(--gap-section)]">
      <header>
        <h1 className="text-phi-2xl font-semibold tracking-tight">{copy.devices.title}</h1>
        <p className="mt-[var(--u-2)] text-phi-sm text-muted-foreground">{copy.devices.subtitle}</p>
      </header>

      <EnrollCard />

      <Card>
        <CardBody className="p-0">
          <div className="flex flex-wrap items-center justify-between gap-[var(--u-3)] px-[var(--pad-card)] py-[var(--u-4)]">
            <label className="min-w-0 flex-1">
              <span className="sr-only">{copy.devices.search}</span>
              <Input
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                placeholder={copy.devices.search}
                aria-label={copy.devices.search}
              />
            </label>
            <span className="shrink-0 text-phi-sm text-muted-foreground">
              {visible.length} {copy.networks.count}
            </span>
          </div>
          {machines.isLoading ? (
            <div className="space-y-[var(--u-3)] p-[var(--pad-card)]">
              <Skeleton className="h-[var(--u-10)]" />
              <Skeleton className="h-[var(--u-10)]" />
            </div>
          ) : null}
          {!machines.isLoading && rows.length === 0 ? (
            <p className="p-[var(--pad-card)] text-phi-sm text-muted-foreground">{copy.devices.empty}</p>
          ) : null}
          {!machines.isLoading && rows.length > 0 && visible.length === 0 ? (
            <p className="p-[var(--pad-card)] text-phi-sm text-muted-foreground">{copy.devices.searchEmpty}</p>
          ) : null}

          {rows.length > 0 ? (
            <div className="hidden md:block">
              <table className="w-full border-collapse text-left text-phi-base">
                <thead>
                  <tr className="border-b border-border text-phi-sm text-muted-foreground">
                    <th className="px-[var(--pad-card)] py-[var(--u-4)] font-medium">{copy.devices.columns.name}</th>
                    <th className="py-[var(--u-4)] font-medium">{copy.devices.columns.version}</th>
                    <th className="py-[var(--u-4)] font-medium">{copy.devices.instances}</th>
                    <th className="py-[var(--u-4)] font-medium">{copy.devices.columns.address}</th>
                    <th className="py-[var(--u-4)] font-medium">{copy.devices.columns.lastSeen}</th>
                    <th className="px-[var(--pad-card)] py-[var(--u-4)] text-right font-medium">{copy.devices.columns.state}</th>
                    <th className="px-[var(--pad-card)] py-[var(--u-4)] text-right font-medium">{copy.devices.columns.actions}</th>
                  </tr>
                </thead>
                <tbody>
                  {visible.map((machine) => (
                    <tr key={machine.machine_id} className="border-b border-border last:border-0">
                      <td className="px-[var(--pad-card)] py-[var(--u-4)]">
                        <p className="font-medium">{displayName(machine)}</p>
                        <p className="text-phi-sm text-muted-foreground">{machine.machine_id}</p>
                      </td>
                      <td className="py-[var(--u-4)] tabular-nums text-muted-foreground">{machine.easytier_version || '—'}</td>
                      <td className="py-[var(--u-4)] tabular-nums text-muted-foreground">{(machine.running_network_instances || []).length}</td>
                      <td className="py-[var(--u-4)] text-muted-foreground">{machine.client_url || '—'}</td>
                      <td className="py-[var(--u-4)] text-muted-foreground">{formatTime(machine.last_seen)}</td>
                      <td className="px-[var(--pad-card)] py-[var(--u-4)] text-right">
                        <Badge tone={machine.online ? 'success' : 'neutral'}>
                          {machine.online ? copy.devices.online : copy.devices.offline}
                        </Badge>
                      </td>
                      <td className="px-[var(--pad-card)] py-[var(--u-4)]">
                        <div className="flex flex-wrap items-center justify-end gap-[var(--u-2)]">
                          <Link
                            className="text-phi-sm underline underline-offset-4"
                            to={'/devices/' + machine.machine_id}
                          >
                            {copy.devices.detailView}
                          </Link>
                          <DeviceActions machine={machine} />
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : null}

          {rows.length > 0 ? (
            <div className="divide-y divide-border md:hidden">
              {rows.map((machine) => (
                <div key={machine.machine_id} className="space-y-[var(--u-2)] px-[var(--pad-card)] py-[var(--u-4)]">
                  <div className="flex items-center justify-between gap-[var(--u-3)]">
                    <span className="truncate font-medium">{displayName(machine)}</span>
                    <Badge tone={machine.online ? 'success' : 'neutral'}>
                      {machine.online ? copy.devices.online : copy.devices.offline}
                    </Badge>
                  </div>
                  <p className="text-phi-sm text-muted-foreground">
                    {machine.easytier_version || '—'} · {formatTime(machine.last_seen)}
                  </p>
                  <p className="break-all text-phi-sm text-muted-foreground">{machine.client_url || '—'}</p>
                  <DeviceActions machine={machine} />
                </div>
              ))}
            </div>
          ) : null}
        </CardBody>
      </Card>
      <BlockedDevices />

      {selected ? (
        <DeviceRoutes
          machine={rows.find((machine) => machine.machine_id === selected) ?? rows[0]}
          onClose={() => setSelected(null)}
        />
      ) : null}
    </div>
  );
}
