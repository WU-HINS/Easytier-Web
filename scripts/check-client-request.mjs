#!/usr/bin/env node
// Transport contract. Every page relies on request(): it must send same-origin
// credentials, surface the server's JSON message, fall back to the status text,
// and resolve 204 without a body. None of that had an executable check.
const problems = [];
const calls = [];

globalThis.fetch = async (url, init) => {
  calls.push({ url, init });
  const script = globalThis.__nextResponse;
  return script(url, init);
};

const module = await import('../client/src/lib/request.ts');
const { request, ApiError, API_PREFIX } = module;

const respond = (script) => {
  globalThis.__nextResponse = script;
};

const expect = (name, condition, detail) => {
  if (!condition) problems.push(name + (detail ? ': ' + detail : ''));
};

// 1. A successful JSON response is parsed and the request carries what we expect.
respond(async () => ({ ok: true, status: 200, json: async () => ({ role: 'user' }) }));
const payload = await request('/console-info');
expect('json body is parsed', payload && payload.role === 'user', JSON.stringify(payload));
expect('prefix is applied', calls[0].url === API_PREFIX + '/console-info', calls[0].url);
expect('credentials are same-origin', calls[0].init.credentials === 'same-origin', String(calls[0].init.credentials));
expect('content type is json', calls[0].init.headers['Content-Type'] === 'application/json');

// 2. A caller supplied header wins over the default.
calls.length = 0;
respond(async () => ({ ok: true, status: 200, json: async () => ({}) }));
await request('/x', { headers: { 'Content-Type': 'text/plain' } });
expect('caller headers win', calls[0].init.headers['Content-Type'] === 'text/plain');

// 3. An error response with a JSON message surfaces that message.
calls.length = 0;
respond(async () => ({ ok: false, status: 409, statusText: 'Conflict', json: async () => ({ message: 'setup_required' }) }));
let thrown = null;
try {
  await request('/console-info');
} catch (error) {
  thrown = error;
}
expect('ApiError is thrown', thrown instanceof ApiError, String(thrown));
expect('status is carried', thrown && thrown.status === 409, String(thrown && thrown.status));
expect('server message wins', thrown && thrown.message === 'setup_required', thrown && thrown.message);

// 4. An error response without JSON falls back to the status text.
respond(async () => ({ ok: false, status: 502, statusText: 'Bad Gateway', json: async () => { throw new Error('not json'); } }));
thrown = null;
try {
  await request('/x');
} catch (error) {
  thrown = error;
}
expect('status text fallback', thrown && thrown.message === 'Bad Gateway', thrown && thrown.message);

// 5. 204 resolves to undefined instead of trying to parse a body.
respond(async () => ({ ok: true, status: 204, json: async () => { throw new Error('should not read the body'); } }));
const empty = await request('/x');
expect('204 resolves to undefined', empty === undefined, String(empty));

if (problems.length === 0) {
  console.log('client request: transport, error mapping and 204 handling verified');
  process.exit(0);
}
console.log('client request: ' + problems.length + ' problem(s)');
for (const problem of problems) console.log('  ' + problem);
process.exit(1);
