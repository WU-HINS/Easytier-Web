import { useMutation } from "@tanstack/react-query";
import { useState } from "react";
import { copy } from "../../copy/zh-CN";
import { api } from "../../lib/http";
import { Button, Card, CardBody, Input } from "../../ui";

/**
 * Self service password rotation. The admin surface is read only on purpose, so
 * this is the only way a user can replace a leaked password.
 */
export function PasswordCard() {
	const [current, setCurrent] = useState("");
	const [next, setNext] = useState("");
	const [error, setError] = useState<string | null>(null);
	const [done, setDone] = useState(false);

	const change = useMutation({
		mutationFn: () => api.changePassword(current, next),
		onSuccess: () => {
			setCurrent("");
			setNext("");
			setError(null);
			setDone(true);
		},
		onError: (failure: Error) => {
			setDone(false);
			setError(failure.message);
		},
	});

	return (
		<Card>
			<CardBody className="space-y-[var(--u-4)]">
				<div>
					<h2 className="text-phi-lg font-semibold">{copy.account.passwordTitle}</h2>
					<p className="mt-[var(--u-1)] text-phi-sm text-muted-foreground">{copy.account.passwordHint}</p>
				</div>
				<label className="block">
					<span className="mb-[var(--u-2)] block text-phi-sm text-muted-foreground">
						{copy.account.currentPassword}
					</span>
					<Input
						type="password"
						autoComplete="current-password"
						value={current}
						onChange={(event) => setCurrent(event.target.value)}
					/>
				</label>
				<label className="block">
					<span className="mb-[var(--u-2)] block text-phi-sm text-muted-foreground">
						{copy.account.newPassword}
					</span>
					<Input
						type="password"
						autoComplete="new-password"
						value={next}
						onChange={(event) => setNext(event.target.value)}
					/>
				</label>
				<div className="flex items-center gap-[var(--u-3)]">
					<Button
						type="button"
						disabled={!current || !next || change.isPending}
						onClick={() => change.mutate()}
					>
						{copy.common.save}
					</Button>
					{done ? <span className="text-phi-sm text-muted-foreground">{copy.account.passwordChanged}</span> : null}
				</div>
				{error ? <p role="alert" className="text-phi-sm text-destructive">{error}</p> : null}
			</CardBody>
		</Card>
	);
}
