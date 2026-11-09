import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { useCopy } from '../../copy/locale';
import { networksApi } from './api';
import { Button, Card, CardBody, Input } from '../../ui';
import { NetworkAdvanced } from './network-advanced';
import { NetworkSecretPanel } from './network-secret-panel';
import { describeError } from '../../lib/errors';

/**
 * The settings tab: the network's own name and the inherited defaults. The ACL
 * and the credentials live on their own tabs, the way the first console had it.
 */
export function NetworkSettings({ networkID, displayName }: { networkID: string; displayName: string }) {
  const copy = useCopy();
	const queryClient = useQueryClient();
	const [name, setName] = useState(displayName);

	useEffect(() => {
		setName(displayName);
	}, [displayName]);

	const rename = useMutation({
		mutationFn: () => networksApi.update(networkID, { display_name: name.trim() }),
		onSuccess: () => {
			void queryClient.invalidateQueries({ queryKey: ['networks'] });
			void queryClient.invalidateQueries({ queryKey: ['network', networkID] });
		},
	});

	return (
		<div className="space-y-[var(--gap-card)]">
			<Card>
				<CardBody className="space-y-[var(--u-3)]">
					<h2 className="text-phi-xl font-semibold">{copy.networkSettings.title}</h2>
					<div className="flex flex-wrap items-end gap-[var(--u-3)]">
						<label className="min-w-0 flex-1">
							<span className="mb-[var(--u-2)] block text-phi-sm text-muted-foreground">{copy.networkSettings.name}</span>
							<Input value={name} onChange={(event) => setName(event.target.value)} />
						</label>
						<Button
							type="button"
							disabled={!name.trim() || rename.isPending}
							onClick={() => rename.mutate()}
						>
							{copy.common.save}
						</Button>
					</div>
					{rename.isError ? (
						<p role="alert" className="text-phi-sm text-destructive">{describeError(rename.error, copy)}</p>
					) : null}
				</CardBody>
			</Card>
			<NetworkSecretPanel networkID={networkID} />
			<NetworkAdvanced networkID={networkID} />
		</div>
	);
}
