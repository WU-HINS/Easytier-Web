import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState, type FormEvent } from 'react';
import { Link } from 'react-router-dom';
import { copy } from '../../copy/zh-CN';
import { api } from '../../lib/http';
import { Badge, Button, Card, CardBody, Input, Skeleton } from '../../ui';

export function NetworksPage() {
  const queryClient = useQueryClient();
  const networks = useQuery({ queryKey: ['networks'], queryFn: api.networks });
  const [name, setName] = useState('');
  const [cidr, setCidr] = useState('10.42.0.0/24');

  const create = useMutation({
    mutationFn: () => api.createNetwork(name, cidr),
    onSuccess: () => {
      setName('');
      void queryClient.invalidateQueries({ queryKey: ['networks'] });
    },
  });

  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (!name.trim()) return;
    create.mutate();
  };

  const rows = networks.data ?? [];

  return (
    <div className="space-y-[var(--gap-section)]">
      <header className="flex flex-wrap items-end justify-between gap-[var(--gap-card)]">
        <div>
          <h1 className="text-phi-2xl font-semibold tracking-tight">{copy.networks.title}</h1>
          <p className="mt-[var(--u-2)] text-phi-sm text-muted-foreground">{copy.networks.subtitle}</p>
        </div>
      </header>

      <div className="phi-split gap-[var(--gap-card)]">
        <Card>
          <CardBody className="p-0">
            {networks.isLoading ? (
              <div className="space-y-[var(--u-3)] p-[var(--pad-card)]">
                <Skeleton className="h-[var(--u-10)]" />
                <Skeleton className="h-[var(--u-10)]" />
              </div>
            ) : null}
            {!networks.isLoading && rows.length === 0 ? (
              <p className="p-[var(--pad-card)] text-phi-sm text-muted-foreground">{copy.networks.empty}</p>
            ) : null}
            {rows.map((network) => (
              <div key={network.network_id} className="flex items-center justify-between gap-[var(--u-4)] border-b border-border px-[var(--pad-card)] py-[var(--u-5)] last:border-0">
                <div className="min-w-0">
                  <p className="truncate text-phi-base font-medium">{network.display_name}</p>
                  <p className="truncate text-phi-sm text-muted-foreground">
                    {network.network_name} · {network.networking_method}
                  </p>
                </div>
                <div className="flex shrink-0 items-center gap-[var(--u-2)]">
                  {network.secure_mode ? <Badge tone="primary">{copy.networks.secure}</Badge> : null}
                  <Badge>{network.member_count} {copy.networks.members}</Badge>
                  <Link
                    to={'/networks/' + network.network_id}
                    className="text-phi-sm underline underline-offset-4"
                  >
                    {copy.networks.detailManage}
                  </Link>
                </div>
              </div>
            ))}
          </CardBody>
        </Card>

        <Card>
          <CardBody>
            <h2 className="text-phi-lg font-semibold">{copy.networks.create}</h2>
            <form className="mt-[var(--u-5)] space-y-[var(--u-4)]" onSubmit={submit}>
              <label className="block">
                <span className="mb-[var(--u-2)] block text-phi-sm text-muted-foreground">{copy.networks.name}</span>
                <Input value={name} onChange={(event) => setName(event.target.value)} required />
              </label>
              <label className="block">
                <span className="mb-[var(--u-2)] block text-phi-sm text-muted-foreground">{copy.networks.cidr}</span>
                <Input value={cidr} onChange={(event) => setCidr(event.target.value)} required />
              </label>
              {create.isError ? <p role="alert" className="text-phi-sm text-destructive">{(create.error as Error).message}</p> : null}
              <Button type="submit" className="w-full" disabled={create.isPending}>
                {copy.networks.submit}
              </Button>
            </form>
          </CardBody>
        </Card>
      </div>
      
    </div>
  );
}
