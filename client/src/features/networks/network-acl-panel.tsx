import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { useCopy } from '../../copy/locale';
import { networksApi } from './api';
import { Badge, Button, Card, CardBody, Input, Skeleton } from '../../ui';
import { describeError } from '../../lib/errors';
import {
  newRule,
  parsePolicy,
  policyToJson,
  protocolsToText,
  selectorsToText,
  textToProtocols,
  textToSelectors,
  type AclPolicy,
  type AclRule,
} from './acl-model';

/**
 * The access control policy, edited two ways at once: a rule table for the common
 * case and the raw JSON for anything the table does not cover. Both write the
 * same shape, so switching views never loses a field.
 */
export function NetworkAclPanel({ networkID }: { networkID: string }) {
  const copy = useCopy();
  const queryClient = useQueryClient();
  const [policy, setPolicy] = useState<AclPolicy>({ default_action: 'allow', rules: [] });
  const [view, setView] = useState<'rules' | 'json'>('rules');
  const [json, setJson] = useState('');
  const acl = useQuery({ queryKey: ['acl', networkID], queryFn: () => networksApi.aclPolicy(networkID) });

  useEffect(() => {
    if (!acl.data) return;
    const parsed = parsePolicy(acl.data.acl_policy ?? '');
    setPolicy(parsed);
    setJson(policyToJson(parsed));
  }, [acl.data]);

  const save = useMutation({
    mutationFn: () => networksApi.saveAclPolicy(networkID, view === 'json' ? json : policyToJson(policy)),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['acl', networkID] });
    },
  });

  const patchRule = (index: number, patch: Partial<AclRule>) => {
    setPolicy((current) => {
      const rules = current.rules.map((rule, at) => (at === index ? { ...rule, ...patch } : rule));
      return { ...current, rules };
    });
  };

  const removeRule = (index: number) => {
    setPolicy((current) => ({ ...current, rules: current.rules.filter((_, at) => at !== index) }));
  };

  const moveRule = (index: number, delta: number) => {
    setPolicy((current) => {
      const rules = current.rules.slice();
      const target = index + delta;
      if (target < 0 || target >= rules.length) return current;
      const moved = rules[index];
      rules[index] = rules[target];
      rules[target] = moved;
      return { ...current, rules };
    });
  };

  const addRule = () => {
    setPolicy((current) => ({ ...current, rules: [...current.rules, newRule()] }));
  };

  return (
    <Card>
      <CardBody className="space-y-[var(--u-4)]">
        <div className="flex flex-wrap items-center justify-between gap-[var(--u-3)]">
          <div>
            <h2 className="text-phi-xl font-semibold">{copy.networkSettings.acl}</h2>
            <p className="mt-[var(--u-1)] text-phi-sm text-muted-foreground">{copy.networkSettings.aclHint}</p>
          </div>
          <div className="flex items-center gap-[var(--u-2)]">
            <Button type="button" variant="ghost" onClick={() => setView(view === 'rules' ? 'json' : 'rules')}>
              {view === 'rules' ? copy.networkSettings.aclJsonView : copy.networkSettings.aclRulesView}
            </Button>
            <Button type="button" disabled={save.isPending} onClick={() => save.mutate()}>
              {copy.networkSettings.saveAcl}
            </Button>
          </div>
        </div>

        {acl.isLoading ? <Skeleton className="h-[var(--u-16)]" /> : null}

        {view === 'rules' ? (
          <div className="space-y-[var(--u-4)]">
            <label className="block max-w-screen-sm">
              <span className="mb-[var(--u-2)] block text-phi-sm text-muted-foreground">
                {copy.networkSettings.aclDefaultAction}
              </span>
              <select
                aria-label={copy.networkSettings.aclDefaultAction}
                value={policy.default_action}
                onChange={(event) => setPolicy({ ...policy, default_action: event.target.value })}
                className="w-full rounded-[var(--radius)] border border-input bg-card p-[var(--u-2)] text-phi-sm"
              >
                <option value="allow">{copy.networkSettings.aclAllow}</option>
                <option value="deny">{copy.networkSettings.aclDeny}</option>
              </select>
            </label>

            <div className="flex items-center justify-between gap-[var(--u-3)]">
              <h3 className="text-phi-base font-semibold">
                {copy.networkSettings.aclRules} ({policy.rules.length})
              </h3>
              <Button type="button" variant="ghost" onClick={addRule}>
                {copy.networkSettings.aclAddRule}
              </Button>
            </div>

            {policy.rules.length === 0 ? (
              <p className="text-phi-sm text-muted-foreground">{copy.networkSettings.aclNoRules}</p>
            ) : null}

            {policy.rules.map((rule, index) => (
              <div key={rule.id} className="space-y-[var(--u-3)] rounded-[var(--radius)] border border-border p-[var(--u-3)]">
                <div className="flex flex-wrap items-end gap-[var(--u-3)]">
                  <label className="min-w-0 flex-1">
                    <span className="mb-[var(--u-1)] block text-phi-sm text-muted-foreground">
                      {copy.networkSettings.aclRuleName}
                    </span>
                    <Input value={rule.name} onChange={(event) => patchRule(index, { name: event.target.value })} />
                  </label>
                  <label>
                    <span className="mb-[var(--u-1)] block text-phi-sm text-muted-foreground">
                      {copy.networkSettings.aclAction}
                    </span>
                    <select
                      aria-label={copy.networkSettings.aclAction}
                      value={rule.action}
                      onChange={(event) => patchRule(index, { action: event.target.value })}
                      className="rounded-[var(--radius)] border border-input bg-card p-[var(--u-2)] text-phi-sm"
                    >
                      <option value="allow">{copy.networkSettings.aclAllow}</option>
                      <option value="deny">{copy.networkSettings.aclDeny}</option>
                    </select>
                  </label>
                  <label className="flex items-center gap-[var(--u-2)] text-phi-sm">
                    <input
                      type="checkbox"
                      checked={rule.enabled}
                      onChange={(event) => patchRule(index, { enabled: event.target.checked })}
                    />
                    {copy.networkSettings.aclEnabled}
                  </label>
                  <Badge tone={rule.enabled ? 'success' : 'neutral'}>
                    {rule.action === 'deny' ? copy.networkSettings.aclDeny : copy.networkSettings.aclAllow}
                  </Badge>
                </div>
                <label className="block">
                  <span className="mb-[var(--u-1)] block text-phi-sm text-muted-foreground">
                    {copy.networkSettings.aclSources}
                  </span>
                  <Input
                    value={selectorsToText(rule.sources)}
                    onChange={(event) => patchRule(index, { sources: textToSelectors(event.target.value) })}
                  />
                </label>
                <label className="block">
                  <span className="mb-[var(--u-1)] block text-phi-sm text-muted-foreground">
                    {copy.networkSettings.aclDestinations}
                  </span>
                  <Input
                    value={selectorsToText(rule.destinations)}
                    onChange={(event) => patchRule(index, { destinations: textToSelectors(event.target.value) })}
                  />
                </label>
                <label className="block">
                  <span className="mb-[var(--u-1)] block text-phi-sm text-muted-foreground">
                    {copy.networkSettings.aclProtocols}
                  </span>
                  <Input
                    value={protocolsToText(rule.protocols)}
                    onChange={(event) => patchRule(index, { protocols: textToProtocols(event.target.value) })}
                  />
                </label>
                <div className="flex flex-wrap items-center gap-[var(--u-2)]">
                  <Button type="button" variant="ghost" onClick={() => moveRule(index, -1)}>
                    {copy.networkSettings.aclUp}
                  </Button>
                  <Button type="button" variant="ghost" onClick={() => moveRule(index, 1)}>
                    {copy.networkSettings.aclDown}
                  </Button>
                  <Button type="button" variant="ghost" onClick={() => removeRule(index)}>
                    {copy.networkSettings.aclRemove}
                  </Button>
                </div>
              </div>
            ))}
          </div>
        ) : (
          <textarea
            aria-label={copy.networkSettings.acl}
            value={json}
            onChange={(event) => setJson(event.target.value)}
            spellCheck={false}
            rows={14}
            className="w-full rounded-[var(--radius)] border border-input bg-card p-[var(--u-3)] font-mono text-phi-sm text-foreground"
          />
        )}

        {save.isError ? (
          <p role="alert" className="text-phi-sm text-destructive">
            {describeError(save.error, copy)}
          </p>
        ) : null}
        {save.isSuccess ? <p className="text-phi-sm text-muted-foreground">{copy.networkSettings.saved}</p> : null}
      </CardBody>
    </Card>
  );
}
