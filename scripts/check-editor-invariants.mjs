// The member editor has three properties a screenshot cannot show; losing any of
// them brings back the overwrite or the silent no-op found in round 160.
//
// Round 180 review: matching raw source let ONE comment line satisfy all three.
// Comments are stripped first now, and every pattern has to sit in code position.
import { existsSync, readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const editorPath = join(root, 'client/src/features/networks/member-editor.tsx');

if (!existsSync(editorPath)) {
  console.error('editor: the member editor moved; update this check');
  process.exit(1);
}

const raw = readFileSync(editorPath, 'utf8');
const withoutBlocks = raw.replace(new RegExp('/\\*[\\s\\S]*?\\*/', 'g'), ' ');
const source = withoutBlocks
  .split(String.fromCharCode(10))
  .filter((line) => line.trim().indexOf('//') !== 0)
  .join(String.fromCharCode(10));

const problems = [];

const successAt = source.indexOf('onSuccess');
if (successAt < 0) {
  problems.push('the editor has no onSuccess handler any more');
} else if (source.slice(successAt, successAt + 400).indexOf('advanceBaseline();') < 0) {
  problems.push('onSuccess does not advance the baseline, so a second save resends stale fields');
}

if (source.indexOf('Object.keys(patch).length === 0') < 0) {
  problems.push('an empty patch is still sent, so clearing a field silently no-ops');
}

const dirtyPattern = new RegExp('!dirty[^A-Za-z0-9_]');
if (!dirtyPattern.test(source)) {
  problems.push('the save button is not disabled on the dirty flag itself');
}

if (problems.length > 0) {
  console.error('editor: ' + problems.length + ' problem(s)');
  for (const problem of problems) console.error('  ' + problem);
  process.exit(1);
}
console.log('editor: baseline, empty patch and dirty state are all guarded');
