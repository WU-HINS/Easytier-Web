/**
 * Transport core. Dependency free so the gate can import this file directly
 * (node --experimental-transform-types) and check the error contract every page
 * depends on: JSON messages are surfaced, non JSON bodies fall back to the status
 * text, 204 resolves to undefined, and the console's own origin is used.
 */

export const API_PREFIX = '/api/v1';

export class ApiError extends Error {
  readonly status: number;
  // The coded form lets a page explain the rejection in its own language: the
  // English message is for logs, not for someone reading a Chinese form.
  readonly code: string;
  readonly field: string;
  readonly value: string;

  constructor(status: number, message: string, code = '', field = '', value = '') {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
    this.field = field;
    this.value = value;
  }
}

export async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const response = await fetch(API_PREFIX + path, {
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', ...(init.headers ?? {}) },
    ...init,
  });
  if (!response.ok) {
    let message = response.statusText;
    let code = '';
    let field = '';
    let value = '';
    try {
      const payload = (await response.json()) as { message?: string; code?: string; field?: string; value?: string };
      if (payload?.message) message = payload.message;
      if (payload?.code) code = payload.code;
      if (payload?.field) field = payload.field;
      if (payload?.value) value = payload.value;
    } catch {
      /* body was not JSON */
    }
    throw new ApiError(response.status, message, code, field, value);
  }
  if (response.status === 204) return undefined as T;
  return (await response.json()) as T;
}
