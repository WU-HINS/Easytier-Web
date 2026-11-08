import { useMutation } from '@tanstack/react-query';
import { useState } from 'react';
import { useCopy } from '../../copy/locale';
import { api } from '../../lib/http';
import { Button, Card, CardBody } from '../../ui';
import { describeError } from '../../lib/errors';

/**
 * Rotating the network secret replaces it for the whole network. The new value is
 * shown once because nothing else in the console displays it, and because the
 * only reason to see it is to hand it to a device the console does not manage.
 */
export function NetworkSecretPanel({ networkID }: { networkID: string }) {
  const copy = useCopy();
  const [secret, setSecret] = useState('');
  const [copied, setCopied] = useState(false);
  const rotate = useMutation({
    mutationFn: () => api.rotateNetworkSecret(networkID),
    onSuccess: (result) => {
      setSecret(result.network_secret);
      setCopied(false);
    },
  });

  return (
    <Card>
      <CardBody className="space-y-[var(--u-3)]">
        <div>
          <h2 className="text-phi-xl font-semibold">{copy.networkSettings.secretTitle}</h2>
          <p className="mt-[var(--u-1)] text-phi-sm text-muted-foreground">{copy.networkSettings.secretHint}</p>
        </div>
        <div className="flex flex-wrap items-center gap-[var(--u-2)]">
          <Button
            type="button"
            disabled={rotate.isPending}
            onClick={() => {
              if (window.confirm(copy.networkSettings.secretConfirm)) rotate.mutate();
            }}
          >
            {copy.networkSettings.secretRotate}
          </Button>
          {rotate.isError ? (
            <span role="alert" className="text-phi-sm text-destructive">
              {describeError(rotate.error, copy)}
            </span>
          ) : null}
        </div>
        {secret ? (
          <div className="space-y-[var(--u-2)]">
            <p className="text-phi-sm text-muted-foreground">{copy.networkSettings.secretRotated}</p>
            <div className="flex flex-wrap items-center gap-[var(--u-2)]">
              <code className="min-w-0 flex-1 break-all rounded-[var(--radius)] border border-border bg-card p-[var(--u-2)] font-mono text-phi-sm">
                {secret}
              </code>
              <Button
                type="button"
                variant="ghost"
                onClick={async () => {
                  try {
                    await navigator.clipboard.writeText(secret);
                    setCopied(true);
                  } catch {
                    setCopied(false);
                  }
                }}
              >
                {copied ? copy.networks.tomlCopied : copy.networks.tomlCopy}
              </Button>
            </div>
          </div>
        ) : null}
      </CardBody>
    </Card>
  );
}
