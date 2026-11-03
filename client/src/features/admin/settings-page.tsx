import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useMemo, useState, type FormEvent, type ReactNode } from 'react';
import { ApiError, api, isSecureOrigin, oidcCallbackURL, type AdminSettings } from '../../lib/http';
import { Button, Card, CardBody, Input } from '../../ui';

type SettingsPatch = Partial<Omit<AdminSettings, 'oidc_client_secret_set'>> & { oidc_client_secret?: string; expected_revision?: number };
type Errors = Record<string, string>;

const usernamePattern = /^[a-zA-Z0-9._-]{1,32}$/;

import { copy } from '../../copy/zh-CN';
/** Field level validation, so a save never fails as one opaque error. */
export function validateSettings(draft: AdminSettings): Errors {
  const errors: Errors = {};
  if (!Number.isInteger(draft.max_instances_per_user) || draft.max_instances_per_user < 0) {
    errors.max_instances_per_user = copy.adminSettings.errorMaxInstances;
  }
  if (!Number.isInteger(draft.password_min_length) || draft.password_min_length < 6 || draft.password_min_length > 128) {
    errors.password_min_length = copy.adminSettings.errorPasswordLength;
  }
  if (!Number.isInteger(draft.password_min_classes) || draft.password_min_classes < 1 || draft.password_min_classes > 4) {
    errors.password_min_classes = copy.adminSettings.errorPasswordClasses;
  }
  const names = draft.reserved_usernames.split(',').map((item) => item.trim()).filter(Boolean);
  const seen = new Set<string>();
  for (const name of names) {
    if (!usernamePattern.test(name)) {
      errors.reserved_usernames = copy.adminSettings.errorReservedFormat;
      break;
    }
    const lower = name.toLowerCase();
    if (seen.has(lower)) {
      errors.reserved_usernames = copy.adminSettings.errorReservedDuplicate(name);
      break;
    }
    seen.add(lower);
  }
  if (!Number.isInteger(draft.extra_port) || (draft.extra_port !== 0 && (draft.extra_port < 1024 || draft.extra_port > 65535))) {
    errors.extra_port = copy.adminSettings.errorExtraPort;
  }
  if (draft.oidc_enabled) {
    if (!draft.oidc_issuer) {
      errors.oidc_issuer = copy.adminSettings.errorOidcIssuerRequired;
    } else if (!/^https?:\/\//.test(draft.oidc_issuer)) {
      errors.oidc_issuer = copy.adminSettings.errorOidcIssuerScheme;
    } else if (!draft.oidc_issuer.startsWith('https://') && !/^http:\/\/(localhost|127\.0\.0\.1)/.test(draft.oidc_issuer)) {
      errors.oidc_issuer = copy.adminSettings.errorOidcIssuerHttps;
    }
    if (!draft.oidc_client_id) {
      errors.oidc_client_id = copy.adminSettings.errorOidcClientRequired;
    }
  }
  return errors;
}

export function AdminSettingsPage() {
  const queryClient = useQueryClient();
  const settings = useQuery({ queryKey: ['admin-settings'], queryFn: api.adminSettings });
  const [draft, setDraft] = useState<AdminSettings | null>(null);
  const [feedback, setFeedback] = useState<string | null>(null);
  const [touched, setTouched] = useState<Record<string, boolean>>({});

  useEffect(() => {
    if (settings.data) setDraft(settings.data);
  }, [settings.data]);

  const errors = useMemo<Errors>(() => (draft ? validateSettings(draft) : {}), [draft]);

  const save = useMutation({
    mutationFn: (patch: SettingsPatch) => api.saveAdminSettings(patch),
    onSuccess: (next) => {
      setDraft(next);
      void queryClient.invalidateQueries({ queryKey: ['admin-settings'] });
      setFeedback(copy.adminSettings.saved);
    },
    onError: (failure: Error) => {
      if (failure instanceof ApiError && failure.status === 409) {
        // Someone else saved first: refresh so the admin sees their values and
        // can re-apply deliberately instead of silently overwriting.
        void queryClient.invalidateQueries({ queryKey: ['admin-settings'] });
        setFeedback(copy.adminSettings.revisionConflict);
        return;
      }
      setFeedback(failure.message);
    },
  });

  if (!draft) return <p className="text-phi-sm text-muted-foreground">{settings.isLoading ? copy.adminSettings.loading : copy.adminSettings.readFailed}</p>;

  const update = <K extends keyof AdminSettings>(key: K, value: AdminSettings[K]) => {
    setDraft({ ...draft, [key]: value });
    setTouched({ ...touched, [String(key)]: true });
  };

  const submit = (event: FormEvent) => {
    event.preventDefault();
    setFeedback(null);
    if (Object.keys(errors).length > 0) {
      setTouched(Object.fromEntries(Object.keys(errors).map((key) => [key, true])));
      setFeedback(copy.adminSettings.invalidCount(Object.keys(errors).length));
      return;
    }
    save.mutate({
      max_instances_per_user: draft.max_instances_per_user,
      password_min_length: draft.password_min_length,
      password_min_classes: draft.password_min_classes,
      reserved_usernames: draft.reserved_usernames,
      registration_open: draft.registration_open,
      oidc_enabled: draft.oidc_enabled,
      oidc_issuer: draft.oidc_issuer,
      oidc_client_id: draft.oidc_client_id,
      oidc_button_label: draft.oidc_button_label,
      extra_tcp_enabled: draft.extra_tcp_enabled,
      extra_udp_enabled: draft.extra_udp_enabled,
      extra_port: draft.extra_port,
      expected_revision: draft.revision,
    });
  };

  const errorFor = (key: string) => (touched[key] ? errors[key] : undefined);

  return (
    <form className="space-y-[var(--gap-section)]" onSubmit={submit} noValidate>
      <header>
        <h1 className="text-phi-2xl font-semibold tracking-tight">{copy.adminSettings.title}</h1>
        <p className="mt-[var(--u-2)] text-phi-sm text-muted-foreground">{copy.adminSettings.subtitle}</p>
      </header>

      <div className="phi-split gap-[var(--gap-card)]">
        <Card>
          <CardBody className="space-y-[var(--u-5)]">
            <h2 className="text-phi-lg font-semibold">{copy.adminSettings.sectionRegistration}</h2>
            <Field label={copy.adminSettings.maxInstancesLabel} hint={copy.adminSettings.maxInstancesHint} error={errorFor('max_instances_per_user')}>
              <Input type="number" min={0} value={draft.max_instances_per_user}
                aria-invalid={Boolean(errorFor('max_instances_per_user'))}
                onChange={(event) => update('max_instances_per_user', Number(event.target.value))} />
            </Field>
            <Field label={copy.adminSettings.passwordLengthLabel} error={errorFor('password_min_length')}>
              <Input type="number" min={6} max={128} value={draft.password_min_length}
                aria-invalid={Boolean(errorFor('password_min_length'))}
                onChange={(event) => update('password_min_length', Number(event.target.value))} />
            </Field>
            <Field label={copy.adminSettings.passwordClassesLabel} hint={copy.adminSettings.passwordClassesHint} error={errorFor('password_min_classes')}>
              <Input type="number" min={1} max={4} value={draft.password_min_classes}
                aria-invalid={Boolean(errorFor('password_min_classes'))}
                onChange={(event) => update('password_min_classes', Number(event.target.value))} />
            </Field>
            <Field label={copy.adminSettings.reservedLabel} error={errorFor('reserved_usernames')}>
              <Input value={draft.reserved_usernames}
                aria-invalid={Boolean(errorFor('reserved_usernames'))}
                onChange={(event) => update('reserved_usernames', event.target.value)} />
            </Field>
            <label className="flex items-center gap-[var(--u-3)] text-phi-base">
              <input type="checkbox" checked={draft.registration_open}
                onChange={(event) => update('registration_open', event.target.checked)} />
              {copy.adminSettings.registrationOpen}
            </label>
          </CardBody>
        </Card>

        <Card>
          <CardBody className="space-y-[var(--u-5)]">
            <h2 className="text-phi-lg font-semibold">OIDC</h2>
            <label className="flex items-center gap-[var(--u-3)] text-phi-base">
              <input type="checkbox" checked={draft.oidc_enabled}
                onChange={(event) => update('oidc_enabled', event.target.checked)} />
              {copy.adminSettings.oidcEnabled}
            </label>
            <Field label="Issuer" error={errorFor('oidc_issuer')}>
              <Input value={draft.oidc_issuer} placeholder="https://id.example.com"
                aria-invalid={Boolean(errorFor('oidc_issuer'))}
                onChange={(event) => update('oidc_issuer', event.target.value)} />
            </Field>
            <Field label="Client ID" error={errorFor('oidc_client_id')}>
              <Input value={draft.oidc_client_id}
                aria-invalid={Boolean(errorFor('oidc_client_id'))}
                onChange={(event) => update('oidc_client_id', event.target.value)} />
            </Field>
            <Field label={copy.adminSettings.clientSecret} hint={draft.oidc_client_secret_set ? copy.adminSettings.clientSecretSet : copy.adminSettings.clientSecretUnset}>
              <Input type="password" placeholder={draft.oidc_client_secret_set ? '••••••••' : ''}
                onChange={(event) => save.mutate({ oidc_client_secret: event.target.value })} />
            </Field>
            <Field label={copy.adminSettings.callbackLabel}>
              <div className="flex items-center gap-[var(--u-2)]">
                <code className="flex-1 truncate rounded-md bg-muted px-[var(--u-3)] py-[var(--u-2)] text-phi-sm">
                  {oidcCallbackURL()}
                </code>
                <Button type="button" variant="secondary" size="sm"
                  onClick={() => navigator.clipboard.writeText(oidcCallbackURL())}>{copy.adminSettings.copy}</Button>
              </div>
            </Field>
            {!isSecureOrigin() ? (
              <p role="alert" className="text-phi-sm text-destructive">
                {copy.adminSettings.insecure}
              </p>
            ) : null}
          </CardBody>
        </Card>

        <Card>
          <CardBody className="space-y-[var(--u-5)]">
            <h2 className="text-phi-lg font-semibold">{copy.adminSettings.sectionAccess}</h2>
            <p className="text-phi-sm text-muted-foreground">
              {copy.adminSettings.wsHintPrefix}<code>{copy.adminSettings.wsSample}</code>
            </p>
            <label className="flex items-center gap-[var(--u-3)] text-phi-base">
              <input type="checkbox" checked={draft.extra_tcp_enabled}
                onChange={(event) => update('extra_tcp_enabled', event.target.checked)} />
              {copy.adminSettings.extraTcp}
            </label>
            <label className="flex items-center gap-[var(--u-3)] text-phi-base">
              <input type="checkbox" checked={draft.extra_udp_enabled}
                onChange={(event) => update('extra_udp_enabled', event.target.checked)} />
              {copy.adminSettings.extraUdp}
            </label>
            <p className="text-phi-sm text-muted-foreground">
              {copy.adminSettings.restartHint}
            </p>
            <Field label={copy.adminSettings.extraPortLabel} hint={copy.adminSettings.extraPortHint} error={errorFor('extra_port')}>
              <Input type="number" min={0} max={65535} value={draft.extra_port}
                aria-invalid={Boolean(errorFor('extra_port'))}
                onChange={(event) => update('extra_port', Number(event.target.value))} />
            </Field>
          </CardBody>
        </Card>
      </div>

      <div className="flex items-center gap-[var(--u-4)]">
        <Button type="submit" disabled={save.isPending}>{save.isPending ? copy.adminSettings.saving : copy.adminSettings.save}</Button>
        {feedback ? <span className="text-phi-sm text-muted-foreground">{feedback}</span> : null}
      </div>
    </form>
  );
}

function Field({ label, hint, error, children }: { label: string; hint?: string; error?: string; children: ReactNode }) {
  return (
    <label className="block">
      <span className="mb-[var(--u-2)] block text-phi-sm text-muted-foreground">{label}</span>
      {children}
      {error ? <span className="mt-[var(--u-1)] block text-phi-sm text-destructive">{error}</span> : null}
      {!error && hint ? <span className="mt-[var(--u-1)] block text-phi-sm text-muted-foreground">{hint}</span> : null}
    </label>
  );
}
