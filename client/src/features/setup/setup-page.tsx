import { useState, type FormEvent } from 'react';
import { useNavigate } from 'react-router-dom';
import { useCopy } from '../../copy/locale';
import { Button, Card, Input } from '../../ui';
import { describeError } from '../../lib/errors';
import { useSetupStatus } from '../auth/use-auth';
import { useCreateAdmin } from './use-setup';

/**
 * First-run wizard. It is the only screen reachable while the console is
 * uninitialized, and it creates the single administrator account.
 */
export function SetupPage() {
  const copy = useCopy();
  const navigate = useNavigate();
  const status = useSetupStatus();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [error, setError] = useState<string | null>(null);

  const setup = useCreateAdmin({
    onSuccess: () => navigate('/', { replace: true }),
    onError: (failure: Error) => setError(failure.message),
  });

  const submit = (event: FormEvent) => {
    event.preventDefault();
    setError(null);
    if (password !== confirm) {
      setError(copy.setup.mismatch);
      return;
    }
    setup.mutate({ username, password });
  };

  if (status.isLoading) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <Card className="p-[var(--pad-card)] text-center">
          <p className="text-phi-base text-muted-foreground">{copy.setup.checking}</p>
        </Card>
      </div>
    );
  }

  if (status.isError) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <Card className="p-[var(--pad-card)] text-center">
          <p role="alert" className="text-phi-base text-destructive">{copy.setup.statusFailed}</p>
          <p className="mt-[var(--u-2)] text-phi-sm text-muted-foreground">{describeError(status.error, copy)}</p>
          <Button className="mt-[var(--u-4)]" onClick={() => status.refetch()}>{copy.setup.retry}</Button>
        </Card>
      </div>
    );
  }

  if (status.data?.initialized) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <Card className="p-[var(--pad-card)] text-center">
          <p className="text-phi-lg font-medium">{copy.setup.initialized}</p>
          <Button className="mt-[var(--u-5)]" onClick={() => navigate('/login')}>{copy.setup.goLogin}</Button>
        </Card>
      </div>
    );
  }

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
            {copy.setup.intro}
          </p>
        </div>
      </section>

      <section className="flex items-center justify-center p-[var(--gap-card)]">
        <div className="panel w-full max-w-[calc(var(--u-12)*6.2)] p-[var(--pad-card)]">
          <h1 className="text-phi-xl font-semibold tracking-tight">{copy.setup.title}</h1>
          <p className="mt-[var(--u-2)] text-phi-sm text-muted-foreground">{copy.setup.subtitle}</p>
          <form className="mt-[var(--u-8)] space-y-[var(--u-5)]" onSubmit={submit}>
            <label className="block">
              <span className="mb-[var(--u-2)] block text-phi-sm text-muted-foreground">{copy.setup.username}</span>
              <Input value={username} autoComplete="username" onChange={(event) => setUsername(event.target.value)} required />
            </label>
            <label className="block">
              <span className="mb-[var(--u-2)] block text-phi-sm text-muted-foreground">{copy.setup.password}</span>
              <Input type="password" value={password} autoComplete="new-password"
                onChange={(event) => setPassword(event.target.value)} required />
            </label>
            <label className="block">
              <span className="mb-[var(--u-2)] block text-phi-sm text-muted-foreground">{copy.setup.confirm}</span>
              <Input type="password" value={confirm} autoComplete="new-password"
                onChange={(event) => setConfirm(event.target.value)} required />
            </label>
            {error ? <p role="alert" className="text-phi-sm text-destructive">{error}</p> : null}
            <Button type="submit" size="lg" className="w-full" disabled={setup.isPending}>
              {setup.isPending ? copy.setup.submitting : copy.setup.submit}
            </Button>
          </form>
          <p className="mt-[var(--u-5)] text-phi-sm text-muted-foreground">
            {copy.setup.hint}
          </p>
        </div>
      </section>
    </div>
  );
}
