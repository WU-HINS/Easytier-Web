#!/usr/bin/env node
// README executability check: the flags, routes and scripts the quickstart
// mentions must actually exist. Documentation that teaches a dead path is worse
// than no documentation (r43: --seed-user created an account nobody could use).
import { readFileSync, existsSync, readdirSync } from 'node:fs';
import { join } from 'node:path';

const root = new URL('../', import.meta.url).pathname;
const read = (path) => readFileSync(join(root, path), 'utf8');
const NL = String.fromCharCode(10);
const BT = String.fromCharCode(96);
const problems = [];

// Backticks carry no meaning here; dropping them keeps the matching simple.
const readme = read('README.md').split(BT).join(' ');
const main = read('cmd/easytier-webd/main.go');
const router = read('internal/httpapi/router.go');

const goSources = [];
const goSourceFiles = [];
const walk = (dir) => {
  for (const entry of readdirSync(join(root, dir), { withFileTypes: true })) {
    const relative = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (!['node_modules', 'dist', '.git', 'testdata'].includes(entry.name)) walk(relative);
    } else if (entry.name.endsWith('.go')) {
      goSources.push(read(relative));
      goSourceFiles.push(relative);
    }
  }
};
walk('internal');
walk('cmd');
const allGo = goSources.join(NL);
// Scripts read some variables too (EASYTIER_RACE drives the gate itself).
let scriptText = "";
for (const name of readdirSync(join(root, "scripts"))) {
  if (name.endsWith(".sh") || name.endsWith(".mjs")) scriptText += read(join("scripts", name));
}
const readByAnything = allGo + NL + scriptText;

const shape = (path) => path.split('/')
  .filter((segment) => segment.length > 0)
  .map((segment) => {
	// Documentation writes placeholders as {id}, :id, <id> or NETWORK_ID.
	if (segment.startsWith('{') || segment.startsWith(':') || segment.startsWith('<')) return ':';
	if (/^[A-Z0-9_]+$/.test(segment)) return ':';
	return segment;
  })
  .join('/');

const quoted = (line, from) => {
  const open = line.indexOf('"', from);
  if (open < 0) return null;
  const close = line.indexOf('"', open + 1);
  if (close < 0) return null;
  return line.slice(open + 1, close);
};

// 1. Flags defined in the CLI.
const definedFlags = new Set();
for (const line of main.split(NL)) {
  const at = line.indexOf('flag.');
  if (at < 0) continue;
  const name = quoted(line, at);
  if (name) definedFlags.add(name);
}
for (const match of readme.matchAll(/--([a-z][a-z-]+)/g)) {
  if (!definedFlags.has(match[1])) problems.push('README documents an unknown flag: --' + match[1]);
}

// 2. Routes registered by the HTTP layer.
const routes = new Set();
for (const line of router.split(NL)) {
  const at = line.indexOf('HandleFunc(');
  if (at < 0) continue;
  const spec = quoted(line, at);
  if (!spec) continue;
  const space = spec.indexOf(' ');
  if (space < 0) continue;
  routes.add(spec.slice(0, space) + ' ' + shape(spec.slice(space + 1)));
}

// Routes registered through constants (the OIDC callback does this) are not
// visible as literals in the HandleFunc call, so every quoted /api/v1 string in
// the package counts as a known path.
const knownPaths = new Set();
for (const file of readdirSync(join(root, "internal", "httpapi"))) {
  if (!file.endsWith(".go") || file.endsWith("_test.go")) continue;
  const body = read(join("internal", "httpapi", file));
  let cursor = 0;
  while ((cursor = body.indexOf(String.fromCharCode(34) + "/api/v1", cursor)) >= 0) {
    const close = body.indexOf(String.fromCharCode(34), cursor + 1);
    if (close < 0) break;
    knownPaths.add(shape(body.slice(cursor + 1, close)));
    cursor = close + 1;
  }
}

// 3. Routes referenced by documented curl calls.
const tokens = readme.split(NL).join(" ").split(" ");
for (let index = 0; index < tokens.length; index += 1) {
  const token = tokens[index];
  const at = token.indexOf('/api/v1');
  if (at < 0) continue;
  const method = tokens.slice(Math.max(0, index - 3), index).find((word) =>
    ['GET', 'POST', 'PUT', 'PATCH', 'DELETE'].includes(word)) || 'GET';
  // Trailing punctuation (a full width bracket, a comma) is prose, not path.
  let raw = token.slice(at).split("?")[0];
  while (raw.length > 0 && !/[A-Za-z0-9_/{}<>:-]/.test(raw[raw.length - 1])) raw = raw.slice(0, -1);
  const path = shape(raw);
  if (!routes.has(method + ' ' + path) && !knownPaths.has(path)) {
    problems.push('README calls an unknown route: ' + method + ' ' + token);
  }
}

// 4. Referenced scripts must exist.
let cursor = 0;
while ((cursor = readme.indexOf("scripts/", cursor)) >= 0) {
  let end = cursor + "scripts/".length;
  while (end < readme.length && /[A-Za-z0-9_.-]/.test(readme[end])) end += 1;
  const name = readme.slice(cursor + "scripts/".length, end);
  if (name.includes(".") && !existsSync(join(root, "scripts", name))) {
    problems.push("README references a missing script: scripts/" + name);
  }
  cursor = end;
}

// 5. Documented environment variables must be read by the program.
const envVars = new Set();
for (const match of readme.matchAll(/EASYTIER_[A-Z_]+/g)) envVars.add(match[0]);
for (const name of envVars) {
  if (!readByAnything.includes(name)) problems.push('README documents an env var nobody reads: ' + name);
}

// 6. The reverse direction: every environment variable the program reads must be
// documented. An operator cannot discover a knob that only exists in the source.
const documentedVars = new Set();
for (const match of readme.matchAll(/EASYTIER_[A-Z_]+/g)) documentedVars.add(match[0]);
const readVars = new Set();
for (const file of goSourceFiles) {
  if (file.endsWith('_test.go')) continue;
  for (const match of read(file).matchAll(/EASYTIER_[A-Z_]+/g)) readVars.add(match[0]);
}
for (const name of readVars) {
  if (!documentedVars.has(name)) problems.push('undocumented environment variable: ' + name);
}

// 7. Documented request bodies must be the ones the smoke script sends. The
// README walkthrough is only trustworthy while it matches executable proof;
// comparing keys catches a rename in either place without caring about values.
const smokeScript = read('scripts/smoke-deploy.sh').split(String.fromCharCode(92)).join('');
const documentedKeys = new Set();
for (const match of readme.matchAll(/-d '?([{][^']*[}])/g)) {
	for (const keyMatch of match[1].matchAll(/"([a-z_]+)":/g)) documentedKeys.add(keyMatch[1]);
}
for (const key of documentedKeys) {
	if (!smokeScript.includes('"' + key + '":')) {
		problems.push("README documents a request field the smoke script never sends: " + key);
	}
}

// The charter has to be reachable from the entry document, otherwise nobody
// reads it (found in r100: root.md existed but README never mentioned it).
const charter = read('root.md');
if (!charter.includes('ai-censorship/ledger.md')) problems.push('root.md does not point at the ledger');
for (const doc of ['root.md', 'docs/GOALS.md', 'docs/REVIEW-TEAM.md', 'ai-censorship/ledger.md']) {
  if (!readme.includes(doc)) problems.push('README never links ' + doc);
}

// The compose file is documentation too: a literal EASYTIER_DB_DRIVER there made
// the documented PostgreSQL switch do nothing at all (review team, r134).
const composePath = join(root, 'docker-compose.yml');
if (existsSync(composePath)) {
  const composeLines = readFileSync(composePath, 'utf8').split(String.fromCharCode(10));
  composeLines.forEach((line, index) => {
    for (const key of ['EASYTIER_DB_DRIVER', 'EASYTIER_DB_DSN']) {
      if (line.includes(key + ':') && !line.includes('${')) {
        problems.push('docker-compose.yml:' + (index + 1) + ' hardcodes ' + key + '; interpolate it or the documented switch is a no-op');
      }
    }
  });
}

// The runtime image is distroless and nonroot; a data directory created by the
// image layer would be owned by root and the process could not write its own
// database. Docker cannot run here, so the property is checked statically
// (review team, round 154).
const dockerfilePath = join(root, 'Dockerfile');
if (existsSync(dockerfilePath)) {
  const dockerfile = readFileSync(dockerfilePath, 'utf8');
  if (dockerfile.includes(':nonroot') && !dockerfile.includes('--chown=')) {
    problems.push('Dockerfile runs as nonroot but never chowns the data directory; the volume would be root owned');
  }
  if (!dockerfile.includes('VOLUME [')) {
    problems.push('Dockerfile no longer declares the data volume; the documented deployment would lose the database');
  }
}
if (problems.length === 0) {
  console.log('readme: ' + definedFlags.size + ' flags, ' + routes.size + ' routes, ' + envVars.size + ' env vars verified');
  process.exit(0);
}
console.log('readme: ' + problems.length + ' problem(s)');
for (const problem of problems) console.log('  ' + problem);
process.exit(1);
