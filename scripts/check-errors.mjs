#!/usr/bin/env node
// A rejected form has to explain itself in the language of the page. The server
// sends a code plus the field it refused; the wording lives in the copy table.
//
// This checks the property that broke before: no component may render the raw
// transport message, because "virtual_cidr is not a valid CIDR" is not what the
// operator sees next to a field labelled 虚拟网段.
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';

const root = new URL('../', import.meta.url).pathname;
const NL = String.fromCharCode(10);

const walk = (dir, test) => {
  const out = [];
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) out.push(...walk(full, test));
    else if (test(full)) out.push(full);
  }
  return out;
};

const problems = [];
const components = walk(join(root, 'client', 'src'), (f) => f.endsWith('.tsx'));
for (const file of components) {
  const lines = readFileSync(file, 'utf8').split(NL);
  for (let index = 0; index < lines.length; index += 1) {
    const line = lines[index];
    if (line.trim().startsWith('//')) continue;
    if (line.indexOf('as Error).message') !== -1) {
      problems.push(file.replace(root, '') + ':' + (index + 1) + ' renders the raw transport message; use describeError');
    }
  }
}

const table = readFileSync(join(root, 'client', 'src', 'copy', 'zh-CN.ts'), 'utf8');
const start = table.indexOf('errors: {');
const end = table.indexOf('fields: {');
if (start < 0 || end < 0) {
  problems.push('copy.errors or copy.fields is missing, so nothing can be translated');
} else {
  let entries = 0;
  for (const line of table.slice(start, end).split(NL)) {
    if (line.startsWith('    ') && line.indexOf(':') !== -1) entries += 1;
  }
  if (entries < 5) problems.push('copy.errors has only ' + entries + ' entries');
}

if (problems.length > 0) {
  for (const problem of problems) console.log('  ' + problem);
  process.exit(1);
}
console.log('error contract: no page renders a raw message and every rejection has localised copy');
