import { useMutation } from '@tanstack/react-query';
import { useState, type FormEvent } from 'react';
import { useCopy } from '../../copy/locale';
import { api } from '../../lib/http';
import { Button, Card, CardBody, Input } from '../../ui';
import { describeError } from '../../lib/errors';

/**
 * Change the signed-in account's password. The server checks the current one and
 * applies the configured password policy, so the form only guards the mismatch.
 */
export function PasswordPage() {
  const copy = useCopy();
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [confirm, setConfirm] = useState('');
  const [localError, setLocalError] = useState('');

  const change = useMutation({
    mutationFn: () => api.changePassword(current, next),
    onSuccess: () => {
      setCurrent('');
      setNext('');
      setConfirm('');
      setLocalError('');
    },
  });

  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (next !== confirm) {
      setLocalError(copy.account.passwordMismatch);
      return;
    }
    setLocalError('');
    change.mutate();
  };

  return (
    <div className="space-y-[var(--gap-section)]">
      <header>
        <h1 className="text-phi-2xl font-semibold tracking-tight">{copy.account.passwordTitle}</h1>
        <p className="mt-[var(--u-2)] text-phi-sm text-muted-foreground">{copy.account.passwordHint}</p>
      </header>
      <Card>
        <CardBody>
          <form className="max-w-screen-sm space-y-[var(--u-4)]" onSubmit={submit}>
            <label className="block">
              <span className="mb-[var(--u-2)] block text-phi-sm text-muted-foreground">{copy.account.currentPassword}</span>
              <Input type="password" value={current} onChange={(event) => setCurrent(event.target.value)} required />
            </label>
            <label className="block">
              <span className="mb-[var(--u-2)] block text-phi-sm text-muted-foreground">{copy.account.newPassword}</span>
              <Input type="password" value={next} onChange={(event) => setNext(event.target.value)} required />
            </label>
            <label className="block">
              <span className="mb-[var(--u-2)] block text-phi-sm text-muted-foreground">{copy.account.confirmPassword}</span>
              <Input type="password" value={confirm} onChange={(event) => setConfirm(event.target.value)} required />
            </label>
            {localError ? <p role="alert" className="text-phi-sm text-destructive">{localError}</p> : null}
            {change.isError ? (
              <p role="alert" className="text-phi-sm text-destructive">{describeError(change.error, copy)}</p>
            ) : null}
            {change.isSuccess ? (
              <p className="text-phi-sm text-muted-foreground">{copy.account.passwordChanged}</p>
            ) : null}
            <Button type="submit" disabled={change.isPending}>
              {copy.common.save}
            </Button>
          </form>
        </CardBody>
      </Card>
    </div>
  );
}
