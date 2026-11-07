#!/usr/bin/env node
// Every key in the Chinese table should have an English counterpart. Missing keys
// fall back silently at runtime, which is how a page ends up half translated
// without anyone noticing, so the gap is measured here instead.
import { readFileSync } from 'node:fs';

const root = new URL('../', import.meta.url).pathname;
const NL = String.fromCharCode(10);
const zh = readFileSync(root + 'client/src/copy/zh-CN.ts', 'utf8');
const en = readFileSync(root + 'client/src/copy/locale.tsx', 'utf8');

// The English table starts at its declaration; earlier text is imports and types.
const enTable = en.slice(en.indexOf('const enUS'));

function keysBySection(source) {
  const sections = new Map();
  let current = null;
  for (const line of source.split(NL)) {
    const open = line.match(/^  ([a-zA-Z]+): {/);
    if (open) {
      current = open[1];
      sections.set(current, new Set());
      continue;
    }
    if (line.startsWith('  },') || line.startsWith('};')) {
      current = null;
      continue;
    }
    const key = line.match(/^    ([a-zA-Z]+):/);
    if (key && current) sections.get(current).add(key[1]);
  }
  return sections;
}

const zhSections = keysBySection(zh);
const enSections = keysBySection(enTable);

let missing = 0;
let total = 0;
for (const [section, keys] of zhSections) {
  const translated = enSections.get(section) ?? new Set();
  const gaps = [...keys].filter((key) => !translated.has(key));
  total += keys.size;
  missing += gaps.length;
  const percent = keys.size === 0 ? 100 : Math.round(((keys.size - gaps.length) / keys.size) * 100);
  console.log(section.padEnd(16) + String(percent).padStart(4) + '%  ' + (gaps.length ? gaps.join(', ') : ''));
}

const covered = total === 0 ? 100 : Math.round(((total - missing) / total) * 100);
console.log('---');
console.log('translated ' + (total - missing) + '/' + total + ' keys (' + covered + '%)');
if (missing > 0) {
  console.log('i18n: ' + missing + ' key(s) have no English text');
  process.exit(1);
}
console.log('i18n: every key has English text');
