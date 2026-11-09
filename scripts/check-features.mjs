#!/usr/bin/env node
// Every feature owns its endpoints and its hooks.
//
// The rule (from the HiDNS frontend constraints): a feature that talks to the server
// keeps that traffic in its own api.ts and its query/mutation hooks in use-<domain>.ts,
// so a page file is rendering and nothing else. A page with no server state is exempt
// and says so.
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join, basename } from 'node:path';

const root = new URL('../', import.meta.url).pathname;
const featuresDir = join(root, 'client', 'src', 'features');
const problems = [];
let checked = 0;

for (const name of readdirSync(featuresDir)) {
  const dir = join(featuresDir, name);
  if (!statSync(dir).isDirectory()) continue;
  checked += 1;
  const files = readdirSync(dir);
  const sources = files.filter((f) => f.endsWith('.ts') || f.endsWith('.tsx'));
  const pages = sources.filter((f) => f.endsWith('page.tsx'));
  if (pages.length === 0) {
    problems.push(name + ': has no *page.tsx');
    continue;
  }
  const body = sources.map((f) => readFileSync(join(dir, f), 'utf8')).join(String.fromCharCode(10));
  // A feature is server-backed when it owns an api module or touches the transport.
  const usesServerState = files.includes("api.ts") || body.includes("lib/http");
  const hasApi = files.includes('api.ts');
  const hasHooks = files.some((f) => f.startsWith('use-') && f.endsWith('.ts'));
  if (usesServerState && !hasApi) problems.push(name + ': calls the server but has no api.ts');
  if (usesServerState && !hasHooks) problems.push(name + ': calls the server but has no use-' + name + '.ts');
  const localOnly = /local-only:/.test(body);
  if (!usesServerState && !localOnly) {
    problems.push(name + ': no server state and no "local-only:" note saying why');
  }
}

console.log('features: ' + checked + ' checked');
if (problems.length > 0) {
  for (const problem of problems) console.log('  ' + problem);
  process.exit(1);
}
console.log('features: every feature owns its api, its hooks and its pages');
