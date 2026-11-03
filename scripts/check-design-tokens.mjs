#!/usr/bin/env node
// Static design audit for review role R-A: components must express colour and
// size through the design tokens, not with literals. Catches the slow drift
// where one page quietly hardcodes a hex colour and stops following the theme.
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';

const root = new URL('../client/src/', import.meta.url).pathname;
const skip = new Set([]);

function walk(dir) {
  const out = [];
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) out.push(...walk(full));
    // Stylesheets matter too: a hardcoded colour in globals.css bypasses every
    // token. They used to sit in a skip list that the walk never reached (r132).
    else if (full.endsWith('.tsx') || full.endsWith('.ts') || full.endsWith('.css')) out.push(full);
  }
  return out;
}

const rules = [
  { name: 'hex colour', pattern: /#[0-9a-fA-F]{3,8}\b/g },
  { name: 'rgb()/hsl() literal', pattern: /\b(?:rgb|hsl)a?\(/g },
  { name: 'px literal', pattern: /(?<![\w-])\d+(?:\.\d+)?px\b/g },
  // The golden ratio split lives in styles/globals.css. Spelling it again as
  // 0.618fr or 61.8% in a component means a change to the token will not move
  // the layout (found by review in r131).
  { name: 'inline golden ratio', pattern: /(0[.,]618fr|61[.,]8%|38[.,]2%)/g },
];

const problems = [];
for (const file of walk(root)) {
  const rel = relative(root, file);
  if (skip.has(rel)) continue;
  const raw = readFileSync(file, 'utf8');
  // Blank out comments while keeping the line count, so a doc block that names the
  // ratio is not reported as an inline literal.
  const withoutComments = raw.replace(/\/\*[\s\S]*?\*\//g, (block) => block.replace(/[^\n]/g, ''));
  const lines = withoutComments.split('\n');
  lines.forEach((line, index) => {
    // Comments describe the ratio on purpose; only code is audited.
		// Only a real line comment truncates code; 'https://' inside a string must not.
		const code = line.replace(/(^|[^:])[ 	]*\/\/.*$/, (all, prefix) => prefix);
	// Token definitions are where literals are allowed to live, and a few CSS
	// constructs cannot take a variable at all (media queries, the phi split).
	if (rel.endsWith('.css')) {
		if (code.trim().startsWith('--')) return;
		if (code.includes('@media')) return;
	}
		for (const rule of rules) {
      for (const match of code.matchAll(rule.pattern)) {
        if (match[0] === '0px') continue;
        // Token definitions and CSS variables are allowed by construction.
        // Only the literal itself being a variable reference is exempt.
        // A colour expression built from a token is the goal, not a violation.
		// The golden ratio is spelled once, in the stylesheet that owns it;
		// that is the single source, so only components are audited for it.
		if (rule.name === 'inline golden ratio' && rel.endsWith('.css')) continue;
		if (rule.name === 'rgb()/hsl() literal' && code.indexOf('var(', match.index) === match.index + match[0].length) continue;
		if (code.slice(Math.max(0, match.index - 4), match.index + match[0].length).includes('var(')) continue;
        problems.push({ file: rel, line: index + 1, rule: rule.name, text: match[0], source: line.trim().slice(0, 90) });
      }
    }
  });
}

// The token file itself is the subject of the "design tokens" gate item, so it is
// checked here too: phi must be the documented value and the geometry ladder must
// follow 8 * phi^((n-3)/2). Without this the item only policed components and a
// silently changed ratio would pass (found by scripts/audit-gates.sh).
const tokenFile = readFileSync(join(root, 'ui/tokens.css'), 'utf8');
const phiMatch = tokenFile.match(/--phi:\s*([0-9.]+)\s*;/);
if (!phiMatch) {
  problems.push({ file: 'ui/tokens.css', line: 0, rule: 'phi token', text: 'missing' });
} else {
  const phi = Number(phiMatch[1]);
  if (Math.abs(phi - 1.618034) > 0.000001) {
    problems.push({ file: 'ui/tokens.css', line: 0, rule: 'phi token', text: 'phi = ' + phi });
  }
  // The ladder now runs past 12; derive the upper bound from what the file
  // actually declares so a token added later cannot escape the check.
  const highest = [...tokenFile.matchAll(/--u-([0-9]+):/g)]
    .reduce((max, match) => Math.max(max, Number(match[1])), 0);
  for (let step = 1; step <= highest; step += 1) {
    const declared = tokenFile.match(new RegExp('--u-' + step + ':\\s*([0-9.]+)px'));
    if (!declared) {
      problems.push({ file: 'ui/tokens.css', line: 0, rule: 'ladder', text: '--u-' + step + ' missing' });
      continue;
    }
    const wanted = 8 * Math.pow(phi, (step - 3) / 2);
    if (Math.abs(Number(declared[1]) - wanted) > 1) {
      problems.push({ file: 'ui/tokens.css', line: 0, rule: 'ladder', text: '--u-' + step + ' = ' + declared[1] + 'px, want about ' + wanted.toFixed(1) });
    }
  }
}


// A var() that nobody declares fails at computed-value time and the whole
// declaration silently disappears. r132 tokenised the focus ring and the panel
// border onto three names that were never defined, and every checker stayed
// green (found by the review team in r134).
const declared = new Set();
for (const sheet of ['ui/tokens.css', 'styles/globals.css']) {
  const css = readFileSync(join(root, sheet), 'utf8');
  // Comments must not be able to fake a declaration: strip them before harvesting.
  const cssCode = css.split('/*').map((part, index) => {
    if (index === 0) return part;
    const end = part.indexOf('*/');
    return end < 0 ? '' : part.slice(end + 2);
  }).join(' ');
  for (const match of cssCode.matchAll(/(--[a-z0-9-]+)[ :	]*:/gi)) declared.add(match[1]);
}
if (declared.size < 20) {
  problems.push('only ' + declared.size + ' custom properties declared; the token file is not being read');
} else {
  for (const file of walk(root)) {
    if (!file.endsWith('.css') && !file.endsWith('.tsx') && !file.endsWith('.ts')) continue;
    const source = readFileSync(file, 'utf8');
    const display = file.startsWith(root) ? file.slice(root.length) : file;
    for (const match of source.matchAll(/var\((--[a-z0-9-]+)/gi)) {
      if (!declared.has(match[1])) {
        const line = source.slice(0, match.index).split(String.fromCharCode(10)).length;
        problems.push({ file: display, line: line, rule: 'undeclared custom property', text: match[1], source: 'used but never declared' });
      }
    }
  }
}
if (problems.length === 0) {
  console.log('design tokens: no hardcoded colour or size literals in ' + walk(root).length + ' files');
  process.exit(0);
}

console.log('design tokens: ' + problems.length + ' literal(s) found');
for (const problem of problems.slice(0, 40)) {
  console.log('  ' + problem.file + ':' + problem.line + '  ' + problem.rule + '  ' + problem.text);
  console.log('      ' + problem.source);
}
if (problems.length > 40) console.log('  ... ' + (problems.length - 40) + ' more');
process.exit(1);
