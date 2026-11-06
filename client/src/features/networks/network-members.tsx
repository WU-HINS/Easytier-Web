import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { InstanceDetailPanel } from './instance-detail-panel';
import { AclStatsPanel } from './acl-stats-panel';
import { copy } from '../../copy/zh-CN';
import { api, type NetworkMember } from '../../lib/http';
import { MemberEditor } from './member-editor';
import { MemberOverride } from './member-override';
import { Badge, Button, Card, CardBody, Skeleton } from '../../ui';

/**
 * Member management for one network. Without this the console could create a
 * network but never put a device in it, which made the central network feature
 * unusable from the browser.
 */
export function NetworkMembers({ networkID, onClose }: { networkID: string; onClose?: () => void }) {
  const queryClient = useQueryClient();
  const [pick, setPick] = useState('');
  const [advanced, setAdvanced] = useState<string | null>(null);
  const [statsFor, setStatsFor] = useState<string | null>(null);
  const [instanceFor, setInstanceFor] = useState<string | null>(null);
  const [editing, setEditing] = useState<string | null>(null);

  const members = useQuery({
    queryKey: ['network-members', networkID],
    queryFn: () => api.networkMembers(networkID),
  });
  const machines = useQuery({ queryKey: ['machines'], queryFn: api.machines, refetchInterval: 5_000 });
  const detail = useQuery({
    queryKey: ['network', networkID],
    queryFn: () => api.network(networkID),
  });

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['network-members', networkID] });
    void queryClient.invalidateQueries({ queryKey: ['networks'] });
  };

  const add = useMutation({
    mutationFn: (deviceID: string) => api.addNetworkMembers(networkID, [deviceID]),
    onSuccess: () => {
      setPick('');
      refresh();
    },
  });
  const remove = useMutation({
    mutationFn: (deviceID: string) => api.removeNetworkMember(networkID, deviceID),
    onSuccess: refresh,
  });
  const destroy = useMutation({
    mutationFn: () => api.deleteNetwork(networkID),
    onSuccess: () => {
      refresh();
      onClose?.();
    },
  });

  const current = members.data?.members ?? [];
  const taken = new Set(current.map((member) => member.device_id));
  const candidates = (machines.data ?? []).filter((machine) => !taken.has(machine.machine_id));

  return (
    <Card>
      <CardBody>
        <div className="flex flex-wrap items-center justify-between gap-[var(--u-3)]">
          <h2 className="text-phi-lg font-semibold">{copy.networks.membersTitle}</h2>
          <div className="flex items-center gap-[var(--u-2)]">
            {onClose ? (
              <Button variant="ghost" onClick={onClose}>
                {copy.networks.back}
              </Button>
            ) : null}
            <Button
              variant="ghost"
              disabled={destroy.isPending}
              onClick={() => {
                if (window.confirm(copy.networks.confirmDelete)) destroy.mutate();
              }}
            >
              {copy.networks.deleteNetwork}
            </Button>
          </div>
        </div>

        {detail.data ? (
          <p className="mt-[var(--u-2)] text-phi-sm text-muted-foreground">
            {detail.data.networking_method}
            {' · '}
            {detail.data.virtual_cidr ?? '—'}
            {detail.data.peer_urls && detail.data.peer_urls.length > 0
              ? ' · ' + detail.data.peer_urls.join(', ')
              : ''}
          </p>
        ) : null}
        {detail.isLoading ? <Skeleton className="mt-[var(--u-2)] h-[var(--u-6)]" /> : null}

        {members.data?.compile_error ? (
          <p role="alert" className="mt-[var(--u-4)] text-phi-sm text-destructive">
            {copy.networks.compileError}
            {members.data.compile_error}
          </p>
        ) : null}

        <div className="mt-[var(--u-5)] space-y-[var(--u-3)]">
          {members.isLoading ? <Skeleton className="h-[var(--u-10)]" /> : null}
          {!members.isLoading && current.length === 0 ? (
            <p className="text-phi-sm text-muted-foreground">{copy.networks.noMembers}</p>
          ) : null}
          {current.map((member: NetworkMember) => (
            <div
              key={member.member_id}
              className="flex flex-wrap items-center justify-between gap-[var(--u-3)] border-b border-border pb-[var(--u-3)] last:border-0 last:pb-0"
            >
              <div className="min-w-0">
                <p className="truncate text-phi-base font-medium">{member.hostname || member.device_id}</p>
                <p className="truncate text-phi-sm text-muted-foreground">
                  {member.virtual_ipv4 || '—'} · {member.device_id}
                </p>
              </div>
              <div className="flex shrink-0 items-center gap-[var(--u-2)]">
                {member.temporary ? <Badge>{copy.networks.temporary}</Badge> : null}
                {member.pending ? <Badge>{copy.networks.pendingSync}</Badge> : null}
                {member.running ? <Badge tone="success">{copy.networks.running}</Badge> : null}
                <Badge tone={member.online ? 'primary' : undefined}>
                  {member.online ? copy.networks.online : copy.networks.offline}
                </Badge>
                <Button
                  variant="ghost"
                  onClick={() => setEditing(editing === member.device_id ? null : member.device_id)}
                >
                  {copy.networks.memberEditOpen}
                </Button>
                <Button
                  variant="ghost"
                  onClick={() => setAdvanced(advanced === member.device_id ? null : member.device_id)}
                >
                  {copy.networks.advanced}
                </Button>
                {member.instance_id ? (
                  <Button
                    type="button"
                    variant="ghost"
                    onClick={() => setInstanceFor(instanceFor === member.device_id ? null : member.device_id)}
                  >
                    {copy.networks.instance}
                  </Button>
                ) : null}
                {member.running && member.instance_id ? (
                  <Button
                    type="button"
                    variant="ghost"
                    onClick={() => setStatsFor(statsFor === member.device_id ? null : member.device_id)}
                  >
                    {copy.networks.aclStats}
                  </Button>
                ) : null}
                <Button
                  variant="ghost"
                  disabled={remove.isPending}
                  onClick={() => remove.mutate(member.device_id)}
                >
                  {copy.networks.remove}
                </Button>
              </div>
              {editing === member.device_id ? (
                <div className="w-full">
                  <MemberEditor
                    networkID={networkID}
                    deviceID={member.device_id}
                    hostname={member.hostname}
                    address={member.virtual_ipv4}
                    onDone={() => setEditing(null)}
                  />
                </div>
              ) : null}
              {advanced === member.device_id ? (
                <div className="w-full">
                  <MemberOverride networkID={networkID} deviceID={member.device_id} />
                </div>
              ) : null}
              {statsFor === member.device_id && member.instance_id ? (
                <AclStatsPanel machineID={member.device_id} instanceID={member.instance_id} />
              ) : null}
              {instanceFor === member.device_id && member.instance_id ? (
                <InstanceDetailPanel machineID={member.device_id} instanceID={member.instance_id} />
              ) : null}
            </div>
          ))}
        </div>

        <div className="mt-[var(--u-6)] flex flex-wrap items-end gap-[var(--u-3)]">
          <label className="min-w-0 flex-1">
            <span className="mb-[var(--u-2)] block text-phi-sm text-muted-foreground">
              {copy.networks.chooseDevice}
            </span>
            <select
              value={pick}
              onChange={(event) => setPick(event.target.value)}
              className="h-[var(--u-10)] w-full rounded-[var(--radius)] border border-input bg-card px-[var(--u-3)] text-phi-base text-foreground"
            >
              <option value="">—</option>
              {candidates.map((machine) => (
                <option key={machine.machine_id} value={machine.machine_id}>
                  {machine.alias || machine.hostname || machine.machine_id}
                </option>
              ))}
            </select>
          </label>
          <Button
            type="button"
            disabled={!pick || add.isPending}
            onClick={() => {
              if (pick) add.mutate(pick);
            }}
          >
            {copy.networks.addMember}
          </Button>
        </div>
        {candidates.length === 0 ? (
          <p className="mt-[var(--u-2)] text-phi-sm text-muted-foreground">{copy.networks.noCandidates}</p>
        ) : null}
        {add.isError ? <p role="alert" className="mt-[var(--u-2)] text-phi-sm text-destructive">{(add.error as Error).message}</p> : null}
        {remove.isError ? (
          <p role="alert" className="mt-[var(--u-2)] text-phi-sm text-destructive">{(remove.error as Error).message}</p>
        ) : null}
        {destroy.isError ? (
          <p role="alert" className="mt-[var(--u-2)] text-phi-sm text-destructive">{(destroy.error as Error).message}</p>
        ) : null}
      </CardBody>
    </Card>
  );
}
