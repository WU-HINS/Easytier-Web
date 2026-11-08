import { useMutation, useQuery } from '@tanstack/react-query';
import { ShieldCheck } from 'lucide-react';
import { useState, type FormEvent } from 'react';
import { useNavigate } from 'react-router-dom';
import { useCopy } from '../../copy/locale';
import { absolute, api } from '../../lib/http';
import { Button, Input } from '../../ui';

/**
 * Login screen.
 *
 * Layout: the classic golden-ratio split (61.8% brand / 38.2% form). The ratio
 * is declared once in .phi-split so the brand panel and the card never drift.
 */
export function LoginPage() {
  const copy = useCopy();
  const navigate = useNavigate();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | null>(null);

  const oidc = useQuery({ queryKey: ['oidc-config'], queryFn: api.oidcConfig, staleTime: 60_000 });
  // The console tells us whether self-registration is open, so a fresh
  // deployment has a visible way in instead of a login box and no sign-up.
  const setup = useQuery({ queryKey: ['setup'], queryFn: api.setupStatus, staleTime: 60_000 });
  const [signup, setSignup] = useState(false);

  // First OIDC sign-in lands back here with ?register=1: the identity is verified
  // but has no account, and the name typed now never changes afterwards.
  const registering = new URLSearchParams(window.location.search).get('register') === '1';
  const [newUsername, setNewUsername] = useState('');
  const register = useMutation({
    mutationFn: () => api.registerOidc(newUsername),
    onSuccess: () => navigate('/', { replace: true }),
    onError: (failure: Error) => setError(failure.message || copy.login.registerFailed),
  });
  const submitRegister = (event: FormEvent) => {
    event.preventDefault();
    setError(null);
    register.mutate();
  };

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

  const canRegister = setup.data?.initialized === true && setup.data?.registration_open === true;
  const signUp = useMutation({
    mutationFn: () => api.register(username, password),
    onSuccess: () => login.mutate(),
    onError: (failure: Error) => setError(failure.message || copy.login.registerFailed),
  });
  const submitSignUp = (event: FormEvent) => {
    event.preventDefault();
    setError(null);
    signUp.mutate();
  };

  return (
    <div className="phi-split min-h-screen">
      <section className="relative hidden flex-col justify-center overflow-hidden p-[var(--gap-section)] lg:flex">
        <div className="grid-lines absolute inset-0 opacity-20" aria-hidden />
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
        <div className="panel w-full max-w-[var(--w-auth-card)] p-[var(--pad-card)]">
          <h1 className="text-phi-xl font-semibold tracking-tight">{registering ? copy.login.registerTitle : copy.login.title}</h1>
          {registering ? (
            <p className="mt-[var(--u-2)] text-phi-sm text-muted-foreground">{copy.login.registerHint}</p>
          ) : null}

          {registering ? (
            <form className="mt-[var(--u-8)] space-y-[var(--u-5)]" onSubmit={submitRegister}>
              <label className="block">
                <span className="mb-[var(--u-2)] block text-phi-sm text-muted-foreground">{copy.login.username}</span>
                <Input
                  value={newUsername}
                  autoComplete="username"
                  onChange={(event) => setNewUsername(event.target.value)}
                  required
                />
              </label>
              {error ? <p role="alert" className="text-phi-sm text-destructive">{error}</p> : null}
              <Button type="submit" size="lg" className="w-full" disabled={register.isPending}>
                {register.isPending ? copy.login.submitting : copy.login.registerSubmit}
              </Button>
            </form>
          ) : (
          <form className="mt-[var(--u-8)] space-y-[var(--u-5)]" onSubmit={submit}>
            <div>
              <label htmlFor="login-username" className="mb-[var(--u-2)] block text-phi-sm text-muted-foreground">{copy.login.username}</label>
              <Input
                id="login-username"
                name="username"
                value={username}
                autoComplete="username"
                onChange={(event) => setUsername(event.target.value)}
                required
              />
            </div>
            <div>
              <label htmlFor="login-password" className="mb-[var(--u-2)] block text-phi-sm text-muted-foreground">{copy.login.password}</label>
              <Input
                id="login-password"
                name="password"
                type="password"
                value={password}
                autoComplete="current-password"
                onChange={(event) => setPassword(event.target.value)}
                required
              />
            </div>
            {error ? <p role="alert" className="text-phi-sm text-destructive">{error}</p> : null}
            <Button type="submit" size="lg" className="w-full" disabled={login.isPending}>
              {login.isPending ? copy.login.submitting : copy.login.submit}
            </Button>
          </form>
          )}

          {registering || !canRegister ? null : signup ? (
            <form className="mt-[var(--u-6)] space-y-[var(--u-5)]" onSubmit={submitSignUp}>
              <h2 className="text-phi-xl font-semibold tracking-tight">{copy.login.signUpTitle}</h2>
              <div>
                <label htmlFor="signup-username" className="mb-[var(--u-2)] block text-phi-sm text-muted-foreground">{copy.login.username}</label>
                <Input id="signup-username" name="username" value={username} autoComplete="username" onChange={(event) => setUsername(event.target.value)} required />
              </div>
              <div>
                <label htmlFor="signup-password" className="mb-[var(--u-2)] block text-phi-sm text-muted-foreground">{copy.login.password}</label>
                <Input
                  id="signup-password"
                  name="new-password"
                  type="password"
                  value={password}
                  autoComplete="new-password"
                  onChange={(event) => setPassword(event.target.value)}
                  required
                />
              </div>
              {error ? <p role="alert" className="text-phi-sm text-destructive">{error}</p> : null}
              <Button type="submit" size="lg" className="w-full" disabled={signUp.isPending}>
                {signUp.isPending ? copy.login.submitting : copy.login.signUpSubmit}
              </Button>
              <button
                type="button"
                className="text-phi-sm text-muted-foreground underline underline-offset-4"
                onClick={() => setSignup(false)}
              >
                {copy.login.backToLogin}
              </button>
            </form>
          ) : (
            <p className="mt-[var(--u-4)] text-phi-sm text-muted-foreground">
              {copy.login.noAccount}{' '}
              <button
                type="button"
                className="underline underline-offset-4"
                onClick={() => setSignup(true)}
              >
                {copy.login.signUp}
              </button>
            </p>
          )}

          {registering ? null : (<>
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
          </>)}
        </div>
      </section>
    </div>
  );
}
