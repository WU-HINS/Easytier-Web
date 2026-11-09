/**
 * The ACL editor and the JSON view share this model, so a round trip has to be
 * lossless: the policy the operator sees in the table must be what gets saved.
 * The editor widgets themselves need a browser and are covered by scripts/ui-tour.mjs.
 */
import { describe, expect, it } from 'vitest';
import { emptyPolicy, newRule, parsePolicy, policyToJson } from './acl-model';

describe('parsePolicy', () => {
  it('reads an empty blob as allow-all rather than failing', () => {
    expect(parsePolicy('')).toEqual(emptyPolicy());
    expect(parsePolicy('   ')).toEqual(emptyPolicy());
  });

  it('keeps an unreadable blob from throwing', () => {
    const policy = parsePolicy('{not json');
    expect(policy.default_action).toBe('allow');
    expect(policy.rules).toEqual([]);
  });

  it('round trips a policy through JSON', () => {
    const policy = { default_action: 'deny', rules: [newRule()] };
    const restored = parsePolicy(policyToJson(policy));
    expect(restored.default_action).toBe('deny');
    expect(restored.rules).toHaveLength(1);
  });
});

describe('newRule', () => {
  it('gives every rule a distinct id so two drafts cannot collide', () => {
    expect(newRule().id).not.toBe(newRule().id);
  });
});
