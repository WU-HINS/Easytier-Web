#!/usr/bin/env node
// Documentation consistency check: the charter, the gate script, the ledger and
// the review records must agree with the code. This exists because the review
// process itself drifted twice (r7, r17) and nobody noticed for rounds.
import { readFileSync, existsSync, readdirSync } from 'node:fs';
import { join } from 'node:path';

const root = new URL('../', import.meta.url).pathname;
const read = (path) => readFileSync(join(root, path), 'utf8');
const BT = String.fromCharCode(96);
const NL = String.fromCharCode(10);
const problems = [];

// 1. Every test named in the ledger or the acceptance checklist must exist.
// The checklist is what a reader trusts first, so a stale name there is worse
// than a stale name in a review record.
const goalsDoc = read('docs/GOALS.md');
// 1. Every test named in the ledger must exist in the repository. Names are
// picked up whether or not they are wrapped in backticks.
const ledger = read('ai-censorship/ledger.md');
const cited = new Set();
// Review records count too: a test named only in a round record is still a claim
// that the test exists (r118 found two such names with no code behind them).
const reviewText = readdirSync(join(root, 'ai-censorship'))
  .filter((name) => name.endsWith('.md'))
  .map((name) => read(join('ai-censorship', name)))
  // Indented lines are command samples, not claims: they carry test name prefixes
  // from go test -run filters.
  .map((body) => body.split(NL).filter((line) => !/^( {4}|	)/.test(line)).join(NL))
  .join(NL);
// Names invented on purpose inside negative self tests. Adding one is a
// deliberate act: either write the test or record why the name is fake.
const deliberateFakes = new Set([
  'TestThisTestDoesNotExist',
  'TestGateAuditMissingTestProbe',
  'TestThisChecklistEntryIsStale',
  'TestGuardThenInsertIsNeverChecked',
]);
const pieces = (ledger + NL + goalsDoc + NL + read('root.md') + NL + reviewText).split(/[^A-Za-z0-9_]/);
for (const piece of pieces) {
  if (deliberateFakes.has(piece)) continue;
  if (piece.length > 6 && piece.startsWith('Test')) cited.add(piece);
}

const sources = [];
const walk = (dir, extensions) => {
  for (const entry of readdirSync(join(root, dir), { withFileTypes: true })) {
    const relative = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (!['node_modules', 'dist', '.git', 'testdata'].includes(entry.name)) walk(relative, extensions);
    } else if (extensions.some((ext) => entry.name.endsWith(ext))) {
      sources.push(read(relative));
    }
  }
};
walk('internal', ['.go']);
const allGo = sources.join(NL);
for (const name of cited) {
  if (!allGo.includes('func ' + name + '(')) problems.push('ledger cites a missing test: ' + name);
}

// 2. Gate steps are compared further down (see rule 6): one implementation only.
const charter = read('docs/REVIEW-TEAM.md');
const gate = read('scripts/gate.sh');

// 3. Every script mentioned in the docs must exist.
const scriptNames = new Set();
let cursor = 0;
while (true) {
  const at = charter.indexOf('scripts/', cursor);
  if (at < 0) break;
  let end = at + 'scripts/'.length;
  while (end < charter.length && /[A-Za-z0-9_.-]/.test(charter[end])) end += 1;
  scriptNames.add(charter.slice(at + 'scripts/'.length, end));
  cursor = end;
}
for (const name of scriptNames) {
  if (!name.includes('.') || name.endsWith('.')) continue;
  if (!existsSync(join(root, 'scripts', name))) problems.push('documented script is missing: scripts/' + name);
}

// 4. Review records must be consecutive and referenced from the goals file.
const records = readdirSync(join(root, 'ai-censorship'))
  .filter((name) => name.startsWith('review-') && name.endsWith('.md'))
  .map((name) => {
    const marker = name.lastIndexOf('-r');
    if (marker < 0) return null;
    const digits = name.slice(marker + 2, name.length - 3);
    const value = Number(digits);
    return Number.isFinite(value) && digits.length > 0 ? value : null;
  })
  .filter((value) => value !== null)
  .sort((a, b) => a - b);
for (let round = 2; round <= records[records.length - 1]; round += 1) {
  if (!records.includes(round)) problems.push('review record for round ' + round + ' is missing');
}
const goals = read('docs/GOALS.md');
for (const round of records) {
  if (!goals.includes('r' + round)) problems.push('GOALS.md does not reference round r' + round);
}

// 5. No TODO/FIXME left in the shipped sources.
const markers = ['TODO', 'FIXME', 'XXX'];
const leftovers = [];
const scan = (dir, extensions) => {
  for (const entry of readdirSync(join(root, dir), { withFileTypes: true })) {
    const relative = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (!['node_modules', 'dist', '.git', 'testdata'].includes(entry.name)) scan(relative, extensions);
    } else if (extensions.some((ext) => entry.name.endsWith(ext))) {
      read(relative).split(NL).forEach((line, index) => {
        if (markers.some((marker) => line.includes(marker))) {
          leftovers.push(relative + ':' + (index + 1) + ' ' + line.trim().slice(0, 80));
        }
      });
    }
  }
};
scan('internal', ['.go']);
scan('client/src', ['.ts', '.tsx']);
for (const leftover of leftovers) problems.push('unfinished marker: ' + leftover);

// 6. The charter gate table must list exactly the steps gate.sh runs.
const gateScript = read("scripts/gate.sh");
const runNames = [];
for (const line of gateScript.split(NL)) {
  const trimmed = line.trim();
  if (trimmed.startsWith("run ")) {
    const open = trimmed.indexOf(String.fromCharCode(34));
    const close = trimmed.indexOf(String.fromCharCode(34), open + 1);
    if (open >= 0 && close > open) runNames.push(trimmed.slice(open + 1, close));
  }
  if (trimmed.includes("SKIP") && trimmed.includes("printf")) {
    const pieces = trimmed.split(String.fromCharCode(34));
    for (const piece of pieces) {
      if (piece.length > 0 && !piece.includes("%") && /[a-z]/.test(piece)) runNames.push(piece.trim());
    }
  }
}
const charterText = read("docs/REVIEW-TEAM.md");
const tableNames = [];
let inGateTable = false;
for (const line of charterText.split(NL)) {
  if (line.startsWith("## 3.")) { inGateTable = true; continue; }
  if (inGateTable && line.startsWith("## ")) break;
  if (!inGateTable || !line.startsWith("| ")) continue;
  const cell = line.slice(2).split("|")[0].trim();
  if (cell === "" || cell === "项" || cell.startsWith("---")) continue;
  tableNames.push(cell);
}
for (const name of runNames) {
  if (!tableNames.includes(name)) problems.push("gate.sh runs a step the charter table does not list: " + name);
}
for (const name of tableNames) {
  if (!runNames.includes(name)) problems.push("the charter lists a gate step gate.sh does not run: " + name);
}

// 8. root.md is the project charter: every gate item must be listed there, and
// every test it cites must exist (checked by the shared citation scan below).
const rootSpec = read('root.md');
const gateNames = [...read('scripts/gate.sh').matchAll(/run "([^"]+)"/g)].map((match) => match[1]);
if (gateNames.length === 0) problems.push('gate.sh declares no steps');
for (const name of gateNames) {
  if (!rootSpec.includes(name)) problems.push('root.md does not list the gate item: ' + name);
}
for (const dimension of ['前端美观', '完整从 0 部署', '极端碰撞', '红蓝攻防', '漏洞渗透', '越权']) {
  if (!rootSpec.includes(dimension)) problems.push('root.md does not cover the review dimension: ' + dimension);
}
if (!rootSpec.includes('ai-censorship/ledger.md')) problems.push('root.md does not point at the ledger');
// 9. The deep check log has to keep pointing at rounds that exist, otherwise it
// becomes a list of claims nobody can verify.
const deepChecks = read('docs/DEEP-CHECKS.md');
let deepEntries = 0;
for (const match of deepChecks.matchAll(/\| r(\d+) \|/g)) {
  const round = Number(match[1]);
  if (round === 1) continue;
  deepEntries += 1;
  if (!records.includes(round)) problems.push('DEEP-CHECKS cites round r' + round + ' with no review record');
}
if (deepEntries === 0) problems.push('DEEP-CHECKS.md lists no deep checks');
// 10. Every document under docs/ has to be reachable from the charter. A file
// nobody links is a file nobody reads, and it drifts out of date silently.
const docFiles = readdirSync(join(root, 'docs')).filter((name) => name.endsWith('.md'));
for (const name of docFiles) {
  if (!rootSpec.includes('docs/' + name)) problems.push('docs/' + name + ' is not referenced from root.md');
}
if (docFiles.length < 5) problems.push('docs/ looks empty; the scan is not reading the directory');

// A gate step is only real if its command is real. Before this, check-docs compared
// the name inside the quotes and nothing else, so replacing any command with true
// kept the charter, the gate and the audit green (review team, r134).
const gatePath = join(root, 'scripts/gate.sh');
if (existsSync(gatePath)) {
  const noOps = new Set(['true', 'false', ':', 'echo', 'exit', 'sleep', 'cat', 'printf']);
  for (const line of readFileSync(gatePath, 'utf8').split(String.fromCharCode(10))) {
    const match = line.match(/^[ 	]*run[ 	]+"([^"]+)"[ 	]+(.+)$/);
    if (!match) continue;
    const stepName = match[1];
    const tokens = match[2].trim().split(/[ 	]+/).filter((token) => token.length > 0);
    let index = 0;
    while (index < tokens.length && (tokens[index] === 'env' || /^[A-Za-z_][A-Za-z0-9_]*=/.test(tokens[index]))) index += 1;
    const head = tokens[index] || '';
    if (head === '' || noOps.has(head)) {
      problems.push('gate step "' + stepName + '" runs a shell no-op (' + head + '); it can never fail');
      continue;
    }
    const scriptToken = tokens.slice(index).find((token) => token.endsWith('.sh') || token.endsWith('.mjs'));
    if (scriptToken) {
      const relative = scriptToken.replace(/^[.][/]/, '');
      if (!existsSync(join(root, relative))) {
        problems.push('gate step "' + stepName + '" runs a script that does not exist: ' + scriptToken);
      }
    }
  }
}

if (problems.length === 0) {
  console.log('docs: charter, gate, ledger and review records agree (' + cited.size + ' cited tests, ' + records.length + ' review records)');
  process.exit(0);
}
console.log('docs: ' + problems.length + ' problem(s)');
for (const problem of problems) console.log('  ' + problem);
process.exit(1);
