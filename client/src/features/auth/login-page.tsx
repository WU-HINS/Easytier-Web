import { useMutation, useQuery } from '@tanstack/react-query';
import { ShieldCheck } from 'lucide-react';
import { useState, type FormEvent } from 'react';
import { useNavigate } from 'react-router-dom';
import { copy } from '../../copy/zh-CN';
import { absolute, api } from '../../lib/http';
import { Button, Input } from '../../ui';

/**
 * Login screen.
 *
 * Layout: the classic golden-ratio split (61.8% brand / 38.2% form). The ratio
 * is declared once in .phi-split so the brand panel and the card never drift.
 */
export function LoginPage() {
  const navigate = useNavigate();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | null>(null);

  const oidc = useQuery({ queryKey: ['oidc-config'], queryFn: api.oidcConfig, staleTime: 60_000 });

  const login = useMutation({
    mutationFn: () => api.login(username, password),
    onSuccess: () => navigate('/', { replace: true }),
    onError: (failure: Error) => setError(failure.message || copy.login.failed),
  });

  const submit = (event: FormEvent) => {
    event.preventDefault();
    setError(null);
    login.mutate();
  };

  return (
    <div className="phi-split min-h-screen">
      <section className="relative hidden flex-col justify-between overflow-hidden p-[var(--gap-section)] lg:flex">
        <div className="grid-lines absolute inset-0 opacity-40" aria-hidden />
        <div className="relative">
          <div className="flex items-baseline gap-[var(--u-2)]">
            <span className="text-phi-3xl font-semibold tracking-tight">{copy.brand}</span>
            <span className="text-phi-base text-muted-foreground">{copy.brandTag}</span>
          </div>
          <p className="mt-[var(--u-6)] max-w-[calc(var(--u-12)*7)] text-phi-lg text-muted-foreground">
            {copy.login.subtitle}
          </p>
        </div>
        <div className="relative flex items-center gap-[var(--u-3)] text-phi-sm text-muted-foreground">
          <ShieldCheck size={16} aria-hidden />
          {copy.login.footer}
        </div>
      </section>

      <section className="flex items-center justify-center p-[var(--gap-card)]">
        <div className="panel w-full max-w-[calc(var(--u-12)*6.2)] p-[var(--pad-card)]">
          <h1 className="text-phi-xl font-semibold tracking-tight">{copy.login.title}</h1>
          <p className="mt-[var(--u-2)] text-phi-sm text-muted-foreground">{copy.login.subtitle}</p>

          <form className="mt-[var(--u-8)] space-y-[var(--u-5)]" onSubmit={submit}>
            <label className="block">
              <span className="mb-[var(--u-2)] block text-phi-sm text-muted-foreground">{copy.login.username}</span>
              <Input
                value={username}
                autoComplete="username"
                onChange={(event) => setUsername(event.target.value)}
                required
              />
            </label>
            <label className="block">
              <span className="mb-[var(--u-2)] block text-phi-sm text-muted-foreground">{copy.login.password}</span>
              <Input
                type="password"
                value={password}
                autoComplete="current-password"
                onChange={(event) => setPassword(event.target.value)}
                required
              />
            </label>
            {error ? <p role="alert" className="text-phi-sm text-destructive">{error}</p> : null}
            <Button type="submit" size="lg" className="w-full" disabled={login.isPending}>
              {login.isPending ? copy.login.submitting : copy.login.submit}
            </Button>
          </form>

          {oidc.isLoading ? (
            <p className="mt-[var(--u-4)] text-phi-sm text-muted-foreground">{copy.login.checkingSso}</p>
          ) : null}
          {oidc.isError ? null : oidc.data?.enabled ? (
            <>
              <div className="my-[var(--u-6)] flex items-center gap-[var(--u-3)] text-phi-sm text-muted-foreground">
                <span className="h-px flex-1 bg-border" />
                {copy.login.or}
                <span className="h-px flex-1 bg-border" />
              </div>
              <Button
                variant="secondary"
                size="lg"
                className="w-full"
                onClick={() => window.location.assign(absolute(oidc.data?.login_path ?? '/auth/oidc/login'))}
              >
                {oidc.data.button_label ?? copy.login.sso}
              </Button>
            </>
          ) : null}
        </div>
      </section>
    </div>
  );
}
