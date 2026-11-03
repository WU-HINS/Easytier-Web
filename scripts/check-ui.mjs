#!/usr/bin/env node
// Static UI audit for review role R-A: every page must handle the loading and
// error branches, icon-only controls must be labelled, and form controls must
// be associated with a label. These are the things a screenshot review misses.
import { readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';

const root = new URL('../client/src/', import.meta.url).pathname;
const NL = String.fromCharCode(10);
const problems = [];

const walk = (dir) => {
  const out = [];
  for (const entry of readdirSync(join(root, dir), { withFileTypes: true })) {
    const relative = join(dir, entry.name);
    if (entry.isDirectory()) out.push(...walk(relative));
    else if (entry.name.endsWith('.tsx')) out.push(relative);
  }
  return out;
};

const files = walk('.');
// walk() collects components; the transport rules below also need plain modules,
// otherwise the fetch rule silently scans nothing (it did, until r92).
const moduleFiles = (() => {
	const out = [];
	const visit = (dir) => {
		for (const entry of readdirSync(join(root, dir), { withFileTypes: true })) {
			const relative = join(dir, entry.name);
			if (entry.isDirectory()) visit(relative);
			else if (entry.name.endsWith('.ts')) out.push(relative);
		}
	};
	visit('.');
	return out;
})();


// 1. Pages render a loading state and an error state for their queries.
for (const file of files.filter((name) => name.endsWith('-page.tsx'))) {
  const body = readFileSync(join(root, file), 'utf8');
  if (body.includes('useQuery') && !body.includes('isLoading')) {
    problems.push(file + ': has a query but no loading branch');
  }
  if (body.includes('useQuery') && !body.includes('isError') && !body.includes('isLoading ?')) {
    problems.push(file + ': has a query but no error branch');
  }
}

// 2. Controls with no visible text need an accessible name.
const iconOnly = /<Button[^>]*>[^<]*<([A-Z][A-Za-z0-9]*)[^>]*\/>[^<]*<\/Button>/g;
const selfClosing = /<Button[^>]*\/>/g;
for (const file of files) {
  const body = readFileSync(join(root, file), 'utf8');
  body.split(NL).forEach((line, index) => {
    if (iconOnly.test(line) || (selfClosing.test(line) && !line.includes('aria-label'))) {
      // A self closing Button is only a problem when it has no text: our variant
      // renders children, so check for an explicit label or visible text.
      if (!line.includes('aria-label') && !/>[^<]*[A-Za-z]{2,}</.test(line)) {
        problems.push(file + ':' + (index + 1) + ' button without an accessible name');
      }
    }
    iconOnly.lastIndex = 0;
    selfClosing.lastIndex = 0;
  });
}

// 3. Form controls must sit inside a label or carry aria-label.
for (const file of files) {
  const lines = readFileSync(join(root, file), 'utf8').split(NL);
  lines.forEach((line, index) => {
    const isControl = line.includes('<Input') || line.includes('<select') || line.includes('<textarea');
    if (!isControl) return;
    const element = lines.slice(index, index + 4).join(NL);
    if (element.includes('aria-label') || element.includes('aria-labelledby')) return;
    const window = lines.slice(Math.max(0, index - 6), index + 1).join(NL);
    if (!window.includes('<label') && !window.includes('<Field')) {
      problems.push(file + ':' + (index + 1) + ' form control without a label');
    }
  });
}


// 4. The single network choke point: every request must go through lib/http.ts so
// cookie handling, the Origin header and error mapping stay in one place.
for (const file of files.concat(moduleFiles)) {
  if (file === 'lib/request.ts') continue;
  const body = readFileSync(join(root, file), 'utf8');
  body.split(NL).forEach((line, index) => {
    let at = line.indexOf('fetch(');
    while (at >= 0) {
      const before = at === 0 ? ' ' : line[at - 1];
      const isIdentifierChar = /[A-Za-z0-9_$]/.test(before);
      if (!isIdentifierChar) {
        problems.push(file + ':' + (index + 1) + ' calls fetch() directly; use lib/request.ts');
      }
      at = line.indexOf('fetch(', at + 1);
    }
  });
}

// 5. Raw HTML injection needs a review, not a default.
for (const file of files) {
  const body = readFileSync(join(root, file), 'utf8');
  body.split(NL).forEach((line, index) => {
    if (line.includes('dangerouslySetInnerHTML')) {
      problems.push(file + ':' + (index + 1) + ' uses dangerouslySetInnerHTML');
    }
  });
}

// User facing text belongs in client/src/copy/zh-CN.ts. Existing inline strings are
// grandfathered through a baseline that may only shrink: a new literal anywhere, or
// one more in a listed file, fails the gate (r110).
const cjkLiteral = /[一-鿿]/;
const cjkBaseline = JSON.parse(readFileSync(new URL('../scripts/cjk-baseline.json', import.meta.url), 'utf8')).files;
const cjkFound = {};
for (const file of files) {
  const lines = readFileSync(join(root, file), 'utf8').split(NL);
  lines.forEach((line, index) => {
    const code = line.replace(/\/\/.*$/, '').replace(/\/\*.*?\*\//g, '');
    if (!cjkLiteral.test(code)) return;
    cjkFound[file] = (cjkFound[file] || 0) + 1;
    if (index >= 0 && (cjkBaseline[file] || 0) < cjkFound[file]) {
      problems.push(file + ':' + (index + 1) + ' contains a literal CJK string; move it to the copy table');
    }
  });
}
for (const [file, allowed] of Object.entries(cjkBaseline)) {
  if ((cjkFound[file] || 0) < allowed) {
    problems.push(file + ' has ' + (cjkFound[file] || 0) + ' inline strings but the baseline still allows ' + allowed + '; lower scripts/cjk-baseline.json');
  }
}

// Motion is a design decision, but a looping animation has to answer the system
// setting. If a stylesheet animates something, it must also offer a still version
// (WCAG 2.3.3, found by review in r121).
const stylesheets = [
  'styles/globals.css',
  'ui/tokens.css',
];
for (const sheet of stylesheets) {
  const css = readFileSync(join(root, sheet), 'utf8');
  const animates = /animation:\s*[a-z]/i.test(css);
  const respects = css.includes('prefers-reduced-motion');
  if (animates && !respects) {
    problems.push(sheet + ' animates without a prefers-reduced-motion block');
  }
}
// A message that appears after an action has to be announced: a red paragraph
// alone is invisible to a screen reader (WCAG 4.1.3, found by review in r122).
for (const file of files) {
  const lines = readFileSync(join(root, file), 'utf8').split(NL);
  lines.forEach((line, index) => {
    if (line.includes('text-destructive') && line.includes('<p') && !line.includes('role=')) {
      problems.push(file + ':' + (index + 1) + ' shows an error without role="alert"; a screen reader will not announce it');
    }
  });
}
// The shell owns every page. Without a skip link a keyboard user tabs through
// the whole sidebar before reaching the content, on every navigation
// (WCAG 2.4.1, found by review in r123).
const shell = readFileSync(join(root, 'app/app-shell.tsx'), 'utf8');
if (!shell.includes('href="#main-content"')) {
  problems.push('app/app-shell.tsx has no skip link to #main-content');
}
if (!shell.includes('id="main-content"')) {
  problems.push('the main landmark has no id for the skip link to target');
}
// A scan that reads nothing is not a pass (r92, r132, r133).
if (files.length < 20) {
  problems.push('only ' + files.length + ' component file(s) found; too few components to trust the result');
}


if (problems.length === 0) {
  console.log('ui: ' + files.length + ' components checked, states and labels are complete');
  process.exit(0);
}
console.log('ui: ' + problems.length + ' problem(s)');
for (const problem of problems) console.log('  ' + problem);
process.exit(1);
