#!/usr/bin/env node
// Frontend/backend contract check: every path+method the client calls must have
// a matching route on the server. Catches the class of bug where a screen is
// written against an endpoint that was never implemented (r19: member editing).
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';

const root = new URL('../', import.meta.url).pathname;

function walk(dir, extensionTest) {
  const out = [];
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) out.push(...walk(full, extensionTest));
    else if (extensionTest(full)) out.push(full);
  }
  return out;
}

function normalise(path) {
  return path
    .replace(/\$\{[^}]+\}/g, ':param')
    .replace(/\{[^}]+\}/g, ':param')
    .replace(/\/+$/, '')
    .replace(/^\/+/, '');
}

// --- server routes -----------------------------------------------------------
const serverRoutes = new Set();
for (const file of walk(join(root, 'internal', 'httpapi'), (f) => f.endsWith('.go') && !f.endsWith('_test.go'))) {
  const source = codeLines(readFileSync(file, 'utf8'));
  const pattern = /HandleFunc\(\s*"([A-Z]+)\s+([^"]+)"/g;
  for (const match of source.matchAll(pattern)) {
    const method = match[1];
    const path = normalise(match[2].replace(/^\/api\/v1/, ''));
    serverRoutes.add(method + ' ' + path);
  }
}


// A commented out request is not a call. The "every route has a caller" assertion
// could be silenced by commenting in a fake call, because the scanners read raw
// source (review team, round 150). Drop whole comment lines before scanning.
function codeLines(source) {
  return source
    .split(String.fromCharCode(10))
    .filter((line) => {
      const trimmed = line.trim();
      return !trimmed.startsWith('//') && !trimmed.startsWith('/*') && !trimmed.startsWith('*');
    })
    .join(String.fromCharCode(10));
}

// --- client calls ------------------------------------------------------------
const calls = [];
const QUOTES = [39, 34, 96].map((code) => String.fromCharCode(code));
const CALL = /request<[^>]*>\(/g;

for (const file of walk(join(root, 'client', 'src'), (f) => f.endsWith('.ts') || f.endsWith('.tsx'))) {
  const source = codeLines(readFileSync(file, 'utf8'));
  const matches = [...source.matchAll(CALL)];
  for (let i = 0; i < matches.length; i += 1) {
    const match = matches[i];
    const callStart = match.index + match[0].length;
    const callEnd = i + 1 < matches.length ? matches[i + 1].index : source.length;
    const body = source.slice(callStart, callEnd);

    // First argument, quote aware: literal text plus one placeholder per
    // concatenated expression ("/networks/" + id + "/members/" + device).
    let quote = null;
    let argument = '';
    let cursor = 0;
    for (; cursor < body.length; cursor += 1) {
      const ch = body[cursor];
      if (quote) {
        argument += ch;
        if (ch === quote) quote = null;
        continue;
      }
      if (QUOTES.includes(ch)) {
        quote = ch;
        argument += ch;
        continue;
      }
      if (ch === ',') break;
      argument += ch;
    }

    let path = '';
    let position = 0;
    while (position < argument.length) {
      const ch = argument[position];
      if (QUOTES.includes(ch)) {
        const closing = argument.indexOf(ch, position + 1);
        if (closing < 0) break;
        path += argument.slice(position + 1, closing);
        position = closing + 1;
        continue;
      }
      if (ch === '+') {
        path += ':param';
        position += 1;
        // Skip the whole expression: identifiers and property access only.
        while (position < argument.length && !QUOTES.includes(argument[position]) && argument[position] !== '+') {
          position += 1;
        }
        // Step over the operator that follows the expression: its value is
        // already represented by the placeholder we just emitted.
        if (position < argument.length && argument[position] === '+') position += 1;
        continue;
      }
      position += 1;
    }
    if (path.startsWith('http')) continue;

    const methodMatch = body.slice(cursor).match(/method:\s*['"]([A-Z]+)['"]/);
    const method = methodMatch ? methodMatch[1] : 'GET';
    calls.push({
      method,
      path: normalise(path),
      raw: path,
      file: relative(join(root, 'client', 'src'), file),
    });
  }
}

// A full page navigation is still a client call: the account page sends the
// operator to the OIDC bind endpoint with window.location, and the contract
// report used to list that route as "no client call" (review team, r143).
const NAVIGATION = /['"]([^'"]*\/api\/v1\/[^'"]*)['"]/g;
for (const file of walk(join(root, 'client', 'src'), (f) => f.endsWith('.ts') || f.endsWith('.tsx'))) {
  const source = codeLines(readFileSync(file, 'utf8'));
  for (const match of source.matchAll(NAVIGATION)) {
    const raw = match[1];
    if (!raw.startsWith('/api/v1/')) continue;
    calls.push({
      method: 'GET',
      // normalise() expects the path the request helper takes, i.e. without
      // the /api/v1 prefix the browser URL carries.
      path: normalise(raw.slice('/api/v1'.length)),
      raw: raw,
      file: relative(join(root, 'client', 'src'), file),
    });
  }
}
// Both sides can be empty and the comparison below still "passes". A rename of
// the call helper or the route registrar would silently disable this check
// (found by the review team in r134).
if (calls.length < 20 || serverRoutes.size < 20) {
  console.error('contract: only ' + calls.length + ' client call(s) and ' + serverRoutes.size + ' server route(s); too few calls to compare');
  process.exit(1);
}

const missing = [];
const used = new Set();
for (const call of calls) {
  const key = call.method + ' ' + call.path;
  used.add(key);
  if (!serverRoutes.has(key)) missing.push(call);
}

// A route with no client call is either a browser-less channel we expect, or a
// feature nobody can reach. The list used to be printed and ignored for dozens
// of rounds (review team, r143); it is an assertion now.
const noClientCallIsExpected = new Map([
  ['GET @up', 'device upload channel, called by the daemon not the browser'],
  ['GET @up/:param', 'device upload channel with a username'],
  ['GET auth/oidc/callback', 'the identity provider redirects here'],
  ['GET auth/oidc/login', 'built from the server supplied login_path at runtime'],
  ['GET healthz', 'probed by the operator, not by the page'],
  ['GET auth/logout', 'original console alias; our client posts to auth/logout'],
  ['PUT auth/password', 'original console alias; our client posts to account/password'],
  ['GET auth/check_login_status', 'original console frontend poll; our client reads /api/v1/me'],
  ['GET auth/captcha', 'original console path, answered with enabled=false'],
  ['GET blocked-devices', 'original console path; our client reads machines/blocked'],
  ['DELETE blocked-devices/:param', 'original console path; our client deletes machines/blocked/:id'],
  ['POST generate-config', 'original console config editor; no page uses it yet (docs/FEATURE-MATRIX.md)'],
  ['POST parse-config', 'original console config editor; no page uses it yet'],
  ['GET machines/:param/networks/info', 'original device view; our page reads the network members'],
  ['GET machines/:param/networks/info/:param', 'original device view; single instance info'],
  ['GET machines/:param/networks/config/:param', 'original device view; our page reads the compiled member config'],
  ['POST machines/:param/networks/metas', 'original device view; instance metadata'],
  ['DELETE machines/:param/networks/:param', 'original device view; our page removes the member instead'],
]);
const unused = [...serverRoutes].filter((route) => !used.has(route)).sort();
const unexplained = unused.filter((route) => !noClientCallIsExpected.has(route));

console.log('client calls: ' + calls.length + ', server routes: ' + serverRoutes.size);
if (unused.length > 0) {
  console.log('routes without a client call (' + unused.length + '): ' + unused.join(', '));
}
if (unexplained.length > 0) {
  console.error('contract: ' + unexplained.length + ' route(s) no client reaches and no reason is recorded:');
  for (const route of unexplained) console.error('  ' + route);
  process.exit(1);
}
if (missing.length === 0) {
  console.log('contract: every client call has a server route');
  process.exit(0);
}
console.log('contract: ' + missing.length + ' client call(s) without a server route');
for (const call of missing) {
  console.log('  ' + call.method + ' /api/v1/' + call.path + '   (' + call.file + ')');
}
process.exit(1);
