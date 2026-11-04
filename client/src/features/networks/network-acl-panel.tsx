import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { copy } from '../../copy/zh-CN';
import { api } from '../../lib/http';
import { Button, Card, CardBody, Skeleton } from '../../ui';

/**
 * The access control list, on its own tab like the first console had it. The
 * policy is edited as raw JSON because the language belongs to the agent; the
 * server validates on write and refuses anything the compiler cannot parse.
 */
export function NetworkAclPanel({ networkID }: { networkID: string }) {
	const queryClient = useQueryClient();
	const [policy, setPolicy] = useState('');
	const [textAreaKey, setTextAreaKey] = useState(0);
	const acl = useQuery({ queryKey: ['acl', networkID], queryFn: () => api.aclPolicy(networkID) });

	useEffect(() => {
		if (acl.data) {
			setPolicy(acl.data.acl_policy ?? '');
			setTextAreaKey((key) => key + 1);
		}
	}, [acl.data]);

	const save = useMutation({
		mutationFn: () => api.putAclPolicy(networkID, policy),
		onSuccess: () => {
			void queryClient.invalidateQueries({ queryKey: ['acl', networkID] });
		},
	});

	return (
		<Card>
			<CardBody className="space-y-[var(--u-3)]">
				<h2 className="text-phi-lg font-semibold">{copy.networkSettings.acl}</h2>
				<p className="text-phi-sm text-muted-foreground">{copy.networkSettings.aclHint}</p>
				{acl.isLoading ? <Skeleton className="h-[var(--u-16)]" /> : null}
				<textarea
					aria-label={copy.networkSettings.acl}
					key={textAreaKey}
					value={policy}
					onChange={(event) => setPolicy(event.target.value)}
					spellCheck={false}
					rows={10}
					className="w-full rounded-[var(--radius)] border border-input bg-card p-[var(--u-3)] font-mono text-phi-sm text-foreground"
				/>
				<div className="flex flex-wrap items-center gap-[var(--u-2)]">
					<Button type="button" disabled={save.isPending} onClick={() => save.mutate()}>
						{copy.networkSettings.saveAcl}
					</Button>
					{save.isError ? (
						<span role="alert" className="text-phi-sm text-destructive">
							{(save.error as Error).message}
						</span>
					) : null}
					{save.isSuccess ? <span className="text-phi-sm text-muted-foreground">{copy.networkSettings.saved}</span> : null}
				</div>
			</CardBody>
		</Card>
	);
}
