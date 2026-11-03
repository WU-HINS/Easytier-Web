#!/usr/bin/env node
// Reads the design tokens and checks WCAG contrast for the pairs that carry
// text or form boundaries. Used by review role R-A: "looks fine" is not
// evidence, a ratio is.
import { readFileSync } from 'node:fs';

const tokenFile = new URL('../client/src/ui/tokens.css', import.meta.url);
const css = readFileSync(tokenFile, 'utf8');

function parseBlock(selector) {
  const start = css.indexOf(selector + ' {');
  if (start < 0) throw new Error('selector not found: ' + selector);
  const open = css.indexOf('{', start);
  const close = css.indexOf('}', open);
  const body = css.slice(open + 1, close);
  const values = {};
  for (const line of body.split('\n')) {
    const match = line.match(/--([a-z0-9-]+):\s*([0-9.]+)\s+([0-9.]+)%\s+([0-9.]+)%/i);
    if (match) values[match[1]] = [Number(match[2]), Number(match[3]), Number(match[4])];
  }
  return values;
}

function hslToRgb([h, s, l]) {
  const saturation = s / 100;
  const lightness = l / 100;
  const c = (1 - Math.abs(2 * lightness - 1)) * saturation;
  const x = c * (1 - Math.abs(((h / 60) % 2) - 1));
  const m = lightness - c / 2;
  let rgb;
  if (h < 60) rgb = [c, x, 0];
  else if (h < 120) rgb = [x, c, 0];
  else if (h < 180) rgb = [0, c, x];
  else if (h < 240) rgb = [0, x, c];
  else if (h < 300) rgb = [x, 0, c];
  else rgb = [c, 0, x];
  return rgb.map((value) => Math.round((value + m) * 255));
}

function luminance(rgb) {
  const [r, g, b] = rgb.map((value) => {
    const channel = value / 255;
    return channel <= 0.03928 ? channel / 12.92 : Math.pow((channel + 0.055) / 1.055, 2.4);
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

function contrast(a, b) {
  const la = luminance(hslToRgb(a));
  const lb = luminance(hslToRgb(b));
  const [light, dark] = la > lb ? [la, lb] : [lb, la];
  return (light + 0.05) / (dark + 0.05);
}

const checks = [
  ['foreground', 'background', 4.5, 'body text'],
  ['muted-foreground', 'background', 4.5, 'secondary text'],
  ['muted-foreground', 'muted', 4.5, 'secondary text on muted'],
  ['primary-foreground', 'primary', 4.5, 'button label'],
  ['input', 'card', 3.0, 'form control boundary'],
  // Added in r133: the first five pairs left the most common coloured text
  // unchecked, including the one users see when something goes wrong.
  ['destructive', 'background', 4.5, 'error text'],
  ['card-foreground', 'card', 4.5, 'panel text'],
  ['primary', 'background', 4.5, 'active navigation label'],
  ['ring', 'background', 3.0, 'focus ring against the page'],
];

if (checks.length < 8) {
  console.error('contrast: only ' + checks.length + ' pair(s) declared; too few contrast pairs to mean anything');
  process.exit(1);
}
let failures = 0;
for (const [themeSelector, label] of [[':root', 'light'], ['.dark', 'dark']]) {
  const tokens = parseBlock(themeSelector);
  console.log('\n' + label + ' theme');
  for (const [fg, bg, minimum, what] of checks) {
    if (!tokens[fg] || !tokens[bg]) {
      console.log('  ' + what.padEnd(28) + ' MISSING (token not defined in this theme)');
      failures += 1;
      continue;
    }
    const ratio = contrast(tokens[fg], tokens[bg]);
    const ok = ratio >= minimum;
    if (!ok) failures += 1;
    console.log(
      '  ' + what.padEnd(28) + fg + ' / ' + bg + '  ' +
      ratio.toFixed(2) + ':1  (min ' + minimum + ':1)  ' + (ok ? 'OK' : 'FAIL'),
    );
  }
}

if (failures > 0) {
  console.error('\n' + failures + ' contrast check(s) failed');
  process.exit(1);
}
console.log('\nall contrast checks passed');
