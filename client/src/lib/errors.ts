import type { Copy } from '../copy/locale';
import { ApiError } from './request';

/**
 * Turns a rejected request into a sentence a person can act on.
 *
 * The server sends a machine readable code plus the field and value it refused;
 * the wording lives in the copy table, so a Chinese page never shows
 * "virtual_cidr is not a valid CIDR" next to a field labelled 虚拟网段. An unknown
 * code degrades to a generic sentence with the code in brackets: still diagnosable
 * without putting raw English in front of the operator.
 */
export function describeError(error: unknown, copy: Copy): string {
  if (!(error instanceof ApiError)) {
    return error instanceof Error && error.message ? error.message : copy.errors.generic;
  }
  const field = fieldLabel(error.field, copy);
  const value = error.value;
  const table = copy.errors as Record<string, string>;
  const template = table[error.code];
  if (!template) {
    return error.code ? copy.errors.generic + ' [' + error.code + ']' : copy.errors.generic;
  }
  return template.split('{field}').join(field).split('{value}').join(value);
}

function fieldLabel(field: string, copy: Copy): string {
  const table = copy.fields as Record<string, string>;
  return table[field] ?? table.unknown;
}
