import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { copy } from '../../copy/zh-CN';
import { api } from '../../lib/http';
import { Button, Card, CardBody, Skeleton } from '../../ui';

/**
 * Admission credentials for one network, on their own tab. The secret is shown
 * once, right after it is issued, because the server never returns it again.
 */
export function NetworkCredentialsPanel({ networkID }: { networkID: string }) {
	const queryClient = useQueryClient();
	const [secret, setSecret] = useState('');
	const credentials = useQuery({ queryKey: ['credentials', networkID], queryFn: () => api.credentials(networkID) });

	const refresh = () => {
		void queryClient.invalidateQueries({ queryKey: ['credentials', networkID] });
	};
	const issue = useMutation({
		mutationFn: () => api.createCredential(networkID, { reusable: true }),
		onSuccess: (created) => {
			setSecret(created.credential_secret);
			refresh();
		},
	});
	const revoke = useMutation({
		mutationFn: (credentialID: string) => api.revokeCredential(networkID, credentialID),
		onSuccess: () => {
			setSecret('');
			refresh();
		},
	});

	const issued = credentials.data?.credentials ?? [];

	return (
		<Card>
			<CardBody className="space-y-[var(--u-3)]">
				<div className="flex flex-wrap items-center justify-between gap-[var(--u-3)]">
					<h2 className="text-phi-lg font-semibold">{copy.networkSettings.credentials}</h2>
					<Button type="button" disabled={issue.isPending} onClick={() => issue.mutate()}>
						{copy.networkSettings.issue}
					</Button>
				</div>
				{credentials.isLoading ? <Skeleton className="h-[var(--u-10)]" /> : null}
				{secret ? (
					<p className="break-all text-phi-sm text-muted-foreground">
						{copy.networkSettings.secretOnce}: <span className="font-mono text-foreground">{secret}</span>
					</p>
				) : null}
				<div className="space-y-[var(--u-2)]">
					{issued.length === 0 ? (
						<p className="text-phi-sm text-muted-foreground">{copy.networkSettings.noCredentials}</p>
					) : null}
					{issued.map((credential) => (
						<div
							key={credential.credential_id}
							className="flex flex-wrap items-center justify-between gap-[var(--u-3)] border-b border-border pb-[var(--u-2)] last:border-0"
						>
							<span className="break-all font-mono text-phi-sm">{credential.credential_id}</span>
							<Button type="button" variant="ghost" onClick={() => revoke.mutate(credential.credential_id)}>
								{copy.networkSettings.revoke}
							</Button>
						</div>
					))}
				</div>
				{issue.isError ? (
					<p role="alert" className="text-phi-sm text-destructive">
						{(issue.error as Error).message}
					</p>
				) : null}
			</CardBody>
		</Card>
	);
}
