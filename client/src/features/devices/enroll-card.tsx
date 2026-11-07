import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { useCopy } from '../../copy/locale';
import { api } from '../../lib/http';
import { Button, Card, CardBody, Skeleton } from '../../ui';

/**
 * The command a device runs to join this console. It is built from console-info
 * rather than hard coded, so the port and the account always match the server
 * that is actually running.
 */
export function EnrollCard() {
  const copy = useCopy();
  const info = useQuery({ queryKey: ['console-info'], queryFn: api.consoleInfo, retry: false });
  const [copied, setCopied] = useState(false);

  if (info.isLoading) {
    return <Skeleton className="h-[var(--u-16)]" />;
  }

  if (info.isError || !info.data) {
    return (
      <Card>
        <CardBody>
          <p role="alert" className="text-phi-sm text-destructive">
            {(info.error as Error | null)?.message ?? copy.devices.enrollTitle}
          </p>
        </CardBody>
      </Card>
    );
  }

  const data = info.data;
  const host = window.location.hostname || 'localhost';
  const command =
    'easytier-core --config-server ' +
    data.config_server_protocol +
    '://' +
    host +
    ':' +
    data.config_server_port +
    '/' +
    data.username;

  const copyCommand = async () => {
    try {
      await navigator.clipboard.writeText(command);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  };

  return (
    <Card>
      <CardBody className="space-y-[var(--u-3)]">
        <div>
          <h2 className="text-phi-lg font-semibold">{copy.devices.enrollTitle}</h2>
          <p className="mt-[var(--u-1)] text-phi-sm text-muted-foreground">{copy.devices.enrollHint}</p>
        </div>
        <div className="flex flex-wrap items-center gap-[var(--u-2)]">
          <code className="min-w-0 flex-1 break-all rounded-[var(--radius)] border border-border bg-card p-[var(--u-2)] font-mono text-phi-sm">
            {command}
          </code>
          <Button type="button" variant="ghost" onClick={copyCommand}>
            {copied ? copy.devices.enrollCopied : copy.devices.enrollCopy}
          </Button>
        </div>
      </CardBody>
    </Card>
  );
}
