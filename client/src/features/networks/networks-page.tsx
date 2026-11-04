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
  const [method, setMethod] = useState('Standalone');
  const [peers, setPeers] = useState('');
  const [publicServer, setPublicServer] = useState('');
  const [secure, setSecure] = useState(false);
  const [query, setQuery] = useState('');

  const create = useMutation({
    mutationFn: () =>
      api.createNetwork({
        display_name: name.trim(),
        networking_method: method,
        virtual_cidr: cidr.trim(),
        peer_urls: peers
          .split(/[\n,]/)
          .map((entry) => entry.trim())
          .filter((entry) => entry !== ''),
        public_server_url: publicServer.trim() === '' ? undefined : publicServer.trim(),
        secure_mode: secure,
      }),
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
  // The original console filters as you type and sorts by display name; both are
  // local to the page because the list is small and the server has no query.
  const needle = query.trim().toLocaleLowerCase();
  const visible = rows
    .filter((network) => {
      if (!needle) return true;
      return (
        network.display_name.toLocaleLowerCase().includes(needle) ||
        network.network_name.toLocaleLowerCase().includes(needle)
      );
    })
    .slice()
    .sort((a, b) => a.display_name.localeCompare(b.display_name));

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
          <div className="flex flex-wrap items-center justify-between gap-[var(--u-3)] px-[var(--pad-card)] py-[var(--u-4)]">
            <label className="min-w-0 flex-1">
              <span className="sr-only">{copy.networks.search}</span>
              <Input
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                placeholder={copy.networks.search}
                aria-label={copy.networks.search}
              />
            </label>
            <span className="shrink-0 text-phi-sm text-muted-foreground">
              {visible.length} {copy.networks.count}
            </span>
          </div>
            {networks.isLoading ? (
              <div className="space-y-[var(--u-3)] p-[var(--pad-card)]">
                <Skeleton className="h-[var(--u-10)]" />
                <Skeleton className="h-[var(--u-10)]" />
              </div>
            ) : null}
            {!networks.isLoading && rows.length === 0 ? (
              <p className="p-[var(--pad-card)] text-phi-sm text-muted-foreground">{copy.networks.empty}</p>
            ) : null}
            {!networks.isLoading && rows.length > 0 && visible.length === 0 ? (
              <div className="space-y-[var(--u-2)] p-[var(--pad-card)]">
                <p className="text-phi-sm text-muted-foreground">{copy.networks.searchEmpty}</p>
                <Button variant="ghost" onClick={() => setQuery("")}>
                  {copy.networks.searchClear}
                </Button>
              </div>
            ) : null}
            {visible.map((network) => (
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
              <label className="block">
                <span className="mb-[var(--u-2)] block text-phi-sm text-muted-foreground">{copy.networks.method}</span>
                <select
                  aria-label={copy.networks.method}
                  value={method}
                  onChange={(event) => setMethod(event.target.value)}
                  className="w-full rounded-[var(--radius)] border border-input bg-card p-[var(--u-2)] text-phi-sm"
                >
                  <option value="Standalone">{copy.networks.methodStandalone}</option>
                  <option value="Manual">{copy.networks.methodManual}</option>
                  <option value="Gateway">{copy.networks.methodGateway}</option>
                  <option value="PublicServer">{copy.networks.methodPublic}</option>
                </select>
              </label>
              {method === "Manual" ? (
                <label className="block">
                  <span className="mb-[var(--u-2)] block text-phi-sm text-muted-foreground">{copy.networks.peers}</span>
                  <textarea
                    aria-label={copy.networks.peers}
                    value={peers}
                    rows={3}
                    onChange={(event) => setPeers(event.target.value)}
                    className="w-full rounded-[var(--radius)] border border-input bg-card p-[var(--u-2)] font-mono text-phi-sm"
                  />
                </label>
              ) : null}
              {method === "Gateway" || method === "PublicServer" ? (
                <label className="block">
                  <span className="mb-[var(--u-2)] block text-phi-sm text-muted-foreground">{copy.networks.publicServer}</span>
                  <Input value={publicServer} onChange={(event) => setPublicServer(event.target.value)} />
                </label>
              ) : null}
              <label className="flex items-center gap-[var(--u-2)] text-phi-sm">
                <input type="checkbox" checked={secure} onChange={(event) => setSecure(event.target.checked)} />
                {copy.networks.secureMode}
              </label>
              <p className="text-phi-sm text-muted-foreground">{copy.networks.createHint}</p>
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
