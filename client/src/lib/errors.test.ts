/**
 * Errors reach the operator through describeError; the raw English message must never
 * be what a Chinese page shows. Browser-level behaviour is asserted in
 * scripts/ui-tour.mjs ("rejection is explained in Chinese"); this pins the mapping.
 */
import { describe, expect, it } from 'vitest';
import { describeError } from './errors';
import { ApiError } from './request';
import { copy as zhCN } from '../copy/zh-CN';

describe('describeError', () => {
  it('renders a coded rejection with the label the page uses', () => {
    const failure = new ApiError(400, 'virtual_cidr is required', 'field_required', 'virtual_cidr', '');
    const text = describeError(failure, zhCN);
    expect(text).toContain(zhCN.fields.virtual_cidr);
    expect(text).not.toContain('virtual_cidr is required');
  });

  it('includes the rejected value when the server sent one', () => {
    const failure = new ApiError(400, 'bad', 'invalid_cidr', 'virtual_cidr', '999.1.1.1/99');
    expect(describeError(failure, zhCN)).toContain('999.1.1.1/99');
  });

  it('falls back to a generic sentence plus the code for an unknown one', () => {
    const failure = new ApiError(500, 'boom', 'something_new', '', '');
    const text = describeError(failure, zhCN);
    expect(text).toContain(zhCN.errors.generic);
    expect(text).toContain('something_new');
  });
});
