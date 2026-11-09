/**
 * local-only: this page changes the theme and the background preference, both held
 * in localStorage by lib/theme. It has no server state, so it needs no api.ts or
 * use-*.ts - the feature checker reads this note and stops asking.
 */
import { useState, type FormEvent } from 'react';
import { useCopy } from '../../copy/locale';
import { useTheme, type ThemeMode } from '../../lib/theme';
import { Button, Card, CardBody, Input } from '../../ui';


export function AppearancePage() {
  const copy = useCopy();
const modes: { value: ThemeMode; label: string }[] = [
  { value: 'auto', label: copy.appearance.auto },
  { value: 'light', label: copy.appearance.light },
  { value: 'dark', label: copy.appearance.dark },
];
  const { mode, setMode, background, setBackground } = useTheme();
  const [draft, setDraft] = useState(background ?? '');
  const [feedback, setFeedback] = useState<string | null>(null);

  const submit = (event: FormEvent) => {
    event.preventDefault();
    const ok = setBackground(draft);
    setFeedback(ok ? copy.appearance.saved : copy.appearance.invalid);
  };

  return (
    <div className="space-y-[var(--gap-section)]">
      <header>
        <h1 className="text-phi-3xl font-semibold tracking-tight">{copy.appearance.title}</h1>
        <p className="mt-[var(--u-2)] text-phi-sm text-muted-foreground">{copy.appearance.subtitle}</p>
      </header>

      <div className="phi-split gap-[var(--gap-card)]">
        <Card>
          <CardBody className="space-y-[var(--u-4)]">
            <h2 className="text-phi-xl font-semibold">{copy.common.theme}</h2>
            <div className="flex gap-[var(--u-2)]">
              {modes.map((item) => (
                <Button
                  key={item.value}
                  variant={mode === item.value ? 'primary' : 'secondary'}
                  size="sm"
                  onClick={() => setMode(item.value)}
                >
                  {item.label}
                </Button>
              ))}
            </div>
          </CardBody>
        </Card>

        <Card>
          <CardBody>
            <h2 className="text-phi-xl font-semibold">{copy.common.background}</h2>
            <form className="mt-[var(--u-4)] space-y-[var(--u-3)]" onSubmit={submit}>
              <Input
                aria-label={copy.common.background}
                value={draft}
                placeholder={copy.appearance.placeholder}
                onChange={(event) => setDraft(event.target.value)}
              />
              <Button type="submit" className="w-full">{copy.common.save}</Button>
              {feedback ? <p className="text-phi-sm text-muted-foreground">{feedback}</p> : null}
            </form>
          </CardBody>
        </Card>
      </div>
    </div>
  );
}
