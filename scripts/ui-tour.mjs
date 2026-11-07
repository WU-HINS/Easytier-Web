#!/usr/bin/env node
// One browser pass over every screen the console claims to have, with an explicit
// assertion per feature and a screenshot saved for each. The point is that the six
// capabilities in docs/UI-MATRIX.md are checked by a committed script, not by a
// throwaway one, and that a failure fails the run.
//
//   node scripts/ui-tour.mjs [base-url] [screenshot-dir]
//
// The credentials come from EASYTIER_UI_USER / EASYTIER_UI_PASSWORD and default to
// the demo pair used by the development console.
// playwright-core is a development dependency only. The repo does not install it by
// default, so look in the usual places and let the caller point at a copy.
const candidates = [
  process.env.PLAYWRIGHT_CORE,
  'playwright-core',
  '/tmp/pw/node_modules/playwright-core/index.mjs',
].filter(Boolean);

let chromium;
for (const candidate of candidates) {
  try {
    ({ chromium } = await import(candidate));
    break;
  } catch {
    // try the next candidate
  }
}
if (!chromium) {
  console.error('ui-tour: playwright-core not found; set PLAYWRIGHT_CORE to its module path');
  process.exit(2);
}
import { mkdirSync } from 'node:fs';

const BASE = process.argv[2] || 'http://127.0.0.1:28201';
const SHOTS = process.argv[3] || 'docs/screenshots';
const USER = process.env.EASYTIER_UI_USER || 'root-owner';
const PASSWORD = process.env.EASYTIER_UI_PASSWORD || 'Real-Client-Pass-1';
const CHROME =
  process.env.PW_CHROME || '/root/.cache/ms-playwright/chromium-1248/chrome-linux64/chrome';

mkdirSync(SHOTS, { recursive: true });

const results = [];
function check(name, condition, detail) {
  const passed = Boolean(condition);
  results.push({ name, passed, detail: detail || '' });
  console.log((passed ? 'PASS  ' : 'FAIL  ') + name + (detail ? '  (' + detail + ')' : ''));
  return passed;
}

const browser = await chromium.launch({ executablePath: CHROME, args: ['--no-sandbox'] });
const context = await browser.newContext({ viewport: { width: 1440, height: 1100 }, locale: 'zh-CN' });
const page = await context.newPage();
const consoleErrors = [];
page.on('console', (message) => {
  if (message.type() === 'error') consoleErrors.push(message.text());
});

const shot = async (name, fullPage = true) => {
  await page.screenshot({ path: SHOTS + '/' + name + '.png', fullPage });
};
const text = async () => (await page.locator('body').innerText()).split(String.fromCharCode(10)).join(' ');

await page.goto(BASE + '/login', { waitUntil: 'networkidle' });
await page.waitForTimeout(500);
check('login page renders', (await text()).includes('登录控制台'));
await shot('01-login');

const login = await context.request.post(BASE + '/api/v1/auth/login', { data: { username: USER, password: PASSWORD } });
check('login accepted', login.ok(), String(login.status()));

// --- seed -------------------------------------------------------------------
// The tour needs at least one network to open. Create one through the page (so the
// request carries the same cookies as the UI) when the console is empty, and delete
// it at the end so repeated runs stay idempotent.
let seededNetworkID = null;
await page.goto(BASE + "/networks", { waitUntil: "networkidle" });
await page.waitForTimeout(600);
const seedState = await page.evaluate(async () => {
  const response = await fetch("/api/v1/networks", { credentials: "same-origin" });
  return { status: response.status, body: await response.json() };
});
if (seedState.status !== 200) {
  check("seed: network list readable", false, String(seedState.status));
} else if (!seedState.body.networks || seedState.body.networks.length === 0) {
  const created = await page.evaluate(async () => {
    const response = await fetch("/api/v1/networks", {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ settings: { display_name: "CI 冒烟网", networking_method: "Standalone", virtual_cidr: "10.99.0.0/29" } }),
    });
    return { status: response.status, body: await response.json() };
  });
  seededNetworkID = created.body.network_id || null;
  check("seed network created", created.status < 300 && Boolean(seededNetworkID), String(created.status));
}
// --- dashboard ---------------------------------------------------------------
await page.goto(BASE + '/', { waitUntil: 'networkidle' });
await page.waitForTimeout(800);
const cards = await page.locator('p.text-phi-3xl').allInnerTexts();
check('dashboard shows four counts', cards.length === 4, cards.join('/'));
const machineRequests = [];
page.on('request', (request) => { if (request.url().includes('/api/v1/machines')) machineRequests.push(request.url()); });
await page.getByLabel('自动刷新').uncheck();
const before = machineRequests.length;
await page.waitForTimeout(6000);
check('auto refresh off stops polling', machineRequests.length === before);
await page.getByLabel('自动刷新').check();
await shot('02-dashboard');

// --- devices -----------------------------------------------------------------
await page.goto(BASE + '/devices', { waitUntil: 'networkidle' });
await page.waitForTimeout(700);
const command = await page.locator('code').first().innerText();
check('enrol command carries port and user', command.includes('--config-server') && command.includes(USER), command);
const headers = await page.locator('table thead th').allInnerTexts();
if (headers.length > 0) {
  check('device table has an instance column', headers.includes('实例'), headers.join('/'));
} else {
  const emptyText = (await text()).split(String.fromCharCode(10)).join(' ');
  check('device list shows its empty state', emptyText.includes('尚无设备'), emptyText.slice(0, 80));
}
await page.getByLabel('搜索设备').fill('no-such-device-xyz');
await page.waitForTimeout(400);
if (headers.length > 0) {
  check('device search shows an empty state', (await text()).includes('没有匹配的设备'));
}
await page.getByLabel('搜索设备').fill('');
await shot('03-devices');

// --- networks list -----------------------------------------------------------
await page.goto(BASE + '/networks', { waitUntil: 'networkidle' });
await page.waitForTimeout(600);
check('network list shows a count', (await text()).includes('项'));
const networkEmpty = (await text()).includes('还没有网络');
if (!networkEmpty) {
  await page.getByLabel('搜索网络').fill('zzz-none');
  await page.waitForTimeout(400);
  check('network search shows an empty state', (await text()).includes('没有匹配的网络'));
  await page.getByRole('button', { name: '清空搜索' }).click();
  await page.waitForTimeout(300);
}
await shot('04-networks');

// --- creation wizard ---------------------------------------------------------
const methods = await page.getByLabel('组网方式').locator('option').allInnerTexts();
check('wizard offers four methods', methods.length === 4, methods.join('/'));
await page.getByLabel('组网方式').selectOption('Manual');
await page.waitForTimeout(200);
check('manual method reveals peer field', (await page.getByLabel('节点地址（每行一个）').count()) === 1);
await page.getByLabel('组网方式').selectOption('Standalone');
await page.waitForTimeout(200);
check('standalone hides peer field', (await page.getByLabel('节点地址（每行一个）').count()) === 0);
await shot('04b-network-create-wizard');

// --- network detail: four tabs ----------------------------------------------
await page.getByRole('link', { name: '管理', exact: true }).first().click();
await page.waitForTimeout(1000);
const tabs = await page.getByRole('tab').allInnerTexts();
check('network page has four tabs', tabs.length === 4, tabs.join('/'));

await page.getByRole('tab', { name: '成员', exact: true }).click();
await page.waitForTimeout(500);
await shot('05-network-members');
if ((await page.getByRole('button', { name: '实例', exact: true }).count()) > 0) {
  await page.getByRole('button', { name: '实例', exact: true }).first().click();
  await page.waitForTimeout(1200);
  const detail = await page.locator('body').innerText();
  check('instance detail lists peers, routes and acl', detail.includes('实例详情') && detail.includes('对端') && detail.includes('路由') && detail.includes('ACL 统计'));
check('instance detail exposes the device log level', detail.includes('日志级别'));
  await shot('27-instance-detail');
  await page.getByRole('button', { name: '实例', exact: true }).first().click();
  await page.waitForTimeout(400);
}

// The member override area shows the effective config as TOML.
if ((await page.getByRole('button', { name: '高级配置' }).count()) > 0) {
  await page.getByRole('button', { name: '高级配置' }).first().click();
await page.waitForTimeout(1000);
const toml = await page.getByLabel('生效配置（TOML）').inputValue();
check('member toml shows the effective config', toml.includes('instance_name') && toml.includes('[[peer]]'), toml.split(String.fromCharCode(10))[0]);
await shot('25-member-toml');
const downloadPromise = page.waitForEvent('download', { timeout: 8000 }).catch(() => null);
await page.getByRole('button', { name: '导出配置' }).click();
const download = await downloadPromise;
check('member toml can be exported', Boolean(download && download.suggestedFilename().endsWith('.toml')), download ? download.suggestedFilename() : 'none');
}

await page.getByRole('tab', { name: '访问控制', exact: true }).click();
await page.waitForTimeout(500);
check('acl tab shows the default action selector', (await page.getByLabel('默认动作').count()) === 1);
check('acl tab offers a rule builder', (await page.getByRole('button', { name: '添加规则' }).count()) === 1);
await page.getByRole('button', { name: '保存策略' }).click();
await page.waitForTimeout(1200);
await shot('05a-network-acl');

// The saved policy has to compile: the members endpoint reports a compile_error
// when it does not, which is what a P1 looked like before this check existed.
const networkID = page.url().split('/').pop();
const members = await (await context.request.get(BASE + '/api/v1/networks/' + networkID + '/members')).json();
check('saved acl policy compiles', !members.compile_error, members.compile_error || 'no compile_error');

await page.getByRole('tab', { name: '凭据', exact: true }).click();
await page.waitForTimeout(500);
await page.getByRole('button', { name: '签发凭据' }).click();
await page.waitForTimeout(1200);
check('credentials tab has a table', (await page.locator('table thead th').allInnerTexts()).includes('ACL 组'));
const codes = await page.locator('code').allInnerTexts();
const join = codes.find((entry) => entry.includes('easytier-core')) || '';
check('temporary credential carries a join command', join.includes('--network-name') && join.includes('--network-secret'), join);
await shot('05b-network-credentials');

await page.getByRole('tab', { name: '设置', exact: true }).click();
await page.waitForTimeout(500);
check('settings tab shows the advanced panel', (await text()).includes('高级设置'));
page.on('dialog', (dialog) => dialog.accept());
check('settings tab offers a secret rotation', (await page.getByText('网络密钥').count()) > 0);
await page.getByRole('button', { name: '重新生成密钥' }).click();
await page.waitForTimeout(1500);
const rotated = (await page.locator('code').allInnerTexts()).find((entry) => entry.length >= 30) || '';
check('rotation returns a fresh secret', rotated.length >= 30, rotated ? rotated.slice(0, 8) + '…' : 'none');
await shot('05c-network-settings');

// --- shell: language, blocked devices, password ------------------------------
await page.goto(BASE + '/devices/blocked', { waitUntil: 'networkidle' });
await page.waitForTimeout(500);
check('blocked devices page renders', page.url().endsWith('/devices/blocked'));
await shot('03b-blocked-devices');

await page.goto(BASE + '/account/password', { waitUntil: 'networkidle' });
await page.waitForTimeout(500);
check('password page renders', (await page.getByLabel('当前密码').count()) === 1);
await shot('06a-password-page');

await page.goto(BASE + '/', { waitUntil: 'networkidle' });
await page.waitForTimeout(500);
await page.getByLabel('语言').selectOption('en-US');
await page.waitForTimeout(500);
const englishTitle = await page.locator('h1').first().innerText();
check('language switch reaches English', englishTitle === 'Overview', englishTitle);
await page.goto(BASE + '/devices', { waitUntil: 'networkidle' });
await page.waitForTimeout(600);
const devicesEnglish = await page.getByLabel('Search devices').count();
// The primary button has to stay readable. tailwind-merge used to drop the text
// colour because the type scale shares the text- prefix, and the page still looked
// plausible: dark text on a dark button at 3.56:1.
const contrast = async (theme) => {
  await page.evaluate((value) => window.localStorage.setItem('theme', value), theme);
  await page.goto(BASE + '/networks', { waitUntil: 'networkidle' });
  await page.waitForTimeout(500);
  return page.evaluate(() => {
    const nums = (value) => (value.match(/[0-9.]+/g) || []).map(Number);
    const lum = (c) => {
      const f = c.slice(0, 3).map((v) => { const s = v / 255; return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4); });
      return 0.2126 * f[0] + 0.7152 * f[1] + 0.0722 * f[2];
    };
    const button = document.querySelector('button.bg-primary:not([disabled])');
    if (!button) return null;
    const style = getComputedStyle(button);
    const l1 = lum(nums(style.color));
    const l2 = lum(nums(style.backgroundColor));
    const ratio = (Math.max(l1, l2) + 0.05) / (Math.min(l1, l2) + 0.05);
    return { ratio: Math.round(ratio * 100) / 100, dark: document.documentElement.classList.contains('dark') };
  });
};
const darkReading = await contrast('dark');
check('primary button is readable in the dark theme', darkReading !== null && darkReading.dark && darkReading.ratio >= 4.5, JSON.stringify(darkReading));
const lightReading = await contrast('light');
check('primary button is readable in the light theme', lightReading !== null && !lightReading.dark && lightReading.ratio >= 4.5, JSON.stringify(lightReading));
await page.evaluate(() => window.localStorage.removeItem('theme'));
// A rejected form has to speak the language of the page.
await page.locator('select[aria-label="Language"], select[aria-label="语言"]').first().selectOption('zh-CN');
await page.goto(BASE + '/networks', { waitUntil: 'networkidle' });
await page.waitForTimeout(400);
await page.getByLabel('名称', { exact: true }).fill('UX 错误文案');
await page.getByLabel('虚拟网段').fill('999.1.1.1/99');
await page.getByRole('button', { name: '创建' }).click();
await page.waitForTimeout(900);
const rejection = await page.evaluate(() => { const el = document.querySelector('[role=alert]'); return el ? el.innerText : ''; });
check('rejection is explained in Chinese', rejection.indexOf('虚拟网段') !== -1, rejection.slice(0, 60));
check('rejection does not leak the column name', rejection.indexOf('virtual_cidr') === -1, rejection.slice(0, 60));

// The only way into a network on a phone is this link; it has to be tappable.
await page.setViewportSize({ width: 390, height: 844 });
await page.reload({ waitUntil: 'networkidle' });
await page.waitForTimeout(500);
const targets = await page.evaluate(() => [...document.querySelectorAll('a, button')]
  .map((el) => { const r = el.getBoundingClientRect(); return { text: (el.innerText || '').trim(), w: Math.round(r.width), h: Math.round(r.height) }; })
  .filter((entry) => entry.text === '管理' || entry.text === '文档'));
check('primary actions are tappable on a phone', targets.length > 0 && targets.every((entry) => entry.h >= 40 && entry.w >= 40), JSON.stringify(targets));
await page.setViewportSize({ width: 1440, height: 900 });
check('device page is translated too', devicesEnglish === 1, devicesEnglish ? 'Search devices' : 'still Chinese');
await page.goto(BASE + '/', { waitUntil: 'networkidle' });
await page.waitForTimeout(400);
await shot('02a-dashboard-en');
await page.locator('select[aria-label="Language"], select[aria-label="语言"]').first().selectOption('zh-CN');
await page.waitForTimeout(400);

// Remove the network this run created so the next run starts from the same place.
if (seededNetworkID) {
  const removed = await page.evaluate(async (id) => {
    const response = await fetch("/api/v1/networks/" + id, { method: "DELETE", credentials: "same-origin" });
    return response.status;
  }, seededNetworkID);
  check("seeded network removed", removed < 300, String(removed));
}

const failed = results.filter((entry) => !entry.passed);
if (consoleErrors.length > 0) {
  console.log('console errors: ' + consoleErrors.join(' | '));
}
console.log('---');
console.log(results.length + ' checks, ' + failed.length + ' failed');
await browser.close();
process.exit(failed.length === 0 ? 0 : 1);
