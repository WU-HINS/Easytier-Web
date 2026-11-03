#!/usr/bin/env node
// Bundle sanity check: the SPA that ships must actually be loadable.
//   - every asset referenced by index.html exists (a stale index after a partial
//     build serves a blank page and no test notices)
//   - the entry script is non trivial (a tree shaken bundle means a broken router)
//   - the bundle does not use eval/new Function, which our own CSP forbids
//   - the mount point and the boot script are present
import { readFileSync, existsSync, readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';

const root = new URL('../client/dist/', import.meta.url).pathname;
const problems = [];

if (!existsSync(join(root, 'index.html'))) {
  console.log('bundle: client/dist/index.html is missing (run npm run build)');
  process.exit(1);
}
const html = readFileSync(join(root, 'index.html'), 'utf8');

// 1. Referenced assets must exist.
const refs = [];
let cursor = 0;
while ((cursor = html.indexOf('src="', cursor)) >= 0) {
  const start = cursor + 5;
  const end = html.indexOf('"', start);
  refs.push(html.slice(start, end));
  cursor = end + 1;
}
cursor = 0;
while ((cursor = html.indexOf('href="', cursor)) >= 0) {
  const start = cursor + 6;
  const end = html.indexOf('"', start);
  refs.push(html.slice(start, end));
  cursor = end + 1;
}
for (const ref of refs) {
  if (ref.startsWith('http') || ref.startsWith('data:')) continue;
  const path = ref.startsWith('/') ? ref.slice(1) : ref;
  if (!existsSync(join(root, path))) problems.push('index.html references a missing file: /' + path);
}

// 2. The mount point and the boot script.
if (!html.includes('id="root"')) problems.push('index.html has no #root mount point');
if (!html.includes('/boot.js')) problems.push('index.html does not load /boot.js');
if (!existsSync(join(root, 'boot.js'))) problems.push('client/dist/boot.js is missing');

// 3. The entry bundle must be substantial and CSP clean.
const assetDir = join(root, 'assets');
const scripts = existsSync(assetDir) ? readdirSync(assetDir).filter((name) => name.endsWith('.js')) : [];
if (scripts.length === 0) {
  problems.push('no javascript bundle in client/dist/assets');
}
let totalBytes = 0;
for (const name of scripts) {
  const full = join(assetDir, name);
  const source = readFileSync(full, 'utf8');
  totalBytes += statSync(full).size;
  // Our CSP allows 'self' scripts only: eval and the Function constructor are
  // blocked at runtime, so a dependency using them breaks the console silently.
  if (source.includes('eval(')) problems.push(name + ' uses eval(), which our CSP blocks');
  if (source.includes('new Function(')) problems.push(name + ' uses new Function(), which our CSP blocks');
}
if (totalBytes < 50000) problems.push('the javascript bundle is only ' + totalBytes + ' bytes; the router is probably tree shaken away');

if (problems.length === 0) {
  console.log('bundle: ' + refs.length + ' asset reference(s), ' + Math.round(totalBytes / 1024) + ' kB of javascript, CSP clean');
  process.exit(0);
}
console.log('bundle: ' + problems.length + ' problem(s)');
for (const problem of problems) console.log('  ' + problem);
process.exit(1);
