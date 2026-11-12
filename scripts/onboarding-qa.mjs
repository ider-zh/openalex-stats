// Real-service onboarding acceptance. No substitute statistics or CDN assets.
import { createRequire } from 'node:module';
import fs from 'node:fs/promises';
import path from 'node:path';
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
const require = createRequire(import.meta.url);
const { chromium } = require('../bin/browser-qa/node_modules/playwright');
const base = process.env.QA_URL || 'http://192.168.1.229:8899';
const destination = path.resolve(process.env.QA_OUTPUT || 'research/onboarding-qa');
const key = 'openalex-atlas:onboarding:v1', week = 7 * 24 * 60 * 60 * 1000;
await fs.mkdir(destination, { recursive: true });
const report = { url: base, started_at: new Date().toISOString(), checks: [], errors: [], outside_requests: [], screenshots: [] };
const browser = await chromium.launch({ channel: 'chrome', headless: true });
function track(page) {
  page.on('pageerror', error => report.errors.push(error.message));
  page.on('request', request => { if (/^https?:/.test(request.url()) && new URL(request.url()).origin !== new URL(base).origin) report.outside_requests.push(request.url()); });
}
const snapshot = page => page.evaluate(() => ({ url: location.href, selected: [...document.querySelectorAll('select')].map(e => [e.id, e.value]), buttons: [...document.querySelectorAll('[aria-pressed="true"],[aria-selected="true"]')].map(e => [e.dataset, e.textContent]), checked: [...document.querySelectorAll('input:checked')].map(e => e.value), chart: window.echarts.getInstanceByDom(document.getElementById('chart')).getOption().series.map(s => ({ id: s.id, name: s.name, data: s.data })) }));
async function layout(page, name) {
  const measured = await page.evaluate(() => {
    const panel = document.querySelector('.oa-guide').getBoundingClientRect(), target = document.querySelector('.oa-guide-target').getBoundingClientRect();
    return { panel: { x: panel.x, y: panel.y, right: panel.right, bottom: panel.bottom }, target: { x: target.x, y: target.y, right: target.right, bottom: target.bottom }, width: document.documentElement.clientWidth, height: innerHeight, scrollWidth: document.documentElement.scrollWidth, count: document.querySelectorAll('.oa-guide').length, active: document.activeElement?.className };
  });
  assert.equal(measured.count, 1); assert.ok(measured.panel.x >= 0 && measured.panel.right <= measured.width + 1 && measured.panel.y >= 0 && measured.panel.bottom <= measured.height + 1, `${name} card in viewport`);
  assert.ok(measured.scrollWidth <= measured.width, `${name} no horizontal overflow`);
  assert.ok(measured.target.x >= 0 && measured.target.right <= measured.width + 1 && measured.target.bottom <= measured.height + 1, `${name} highlight clipped`);
  assert.equal(measured.active, 'oa-guide-next');
  const overlaps = measured.panel.x < measured.target.right && measured.panel.right > measured.target.x && measured.panel.y < measured.target.bottom && measured.panel.bottom > measured.target.y;
  assert.equal(overlaps, false, `${name} highlighted control is visible`);
  report.checks.push({ name, measured });
}
async function shot(page, name) { const file = path.join(destination, `${name}.png`); await page.screenshot({ path: file }); report.screenshots.push(file); }
async function exercise(width, metric = '') {
  const label = `${width}-${metric || 'publications'}`;
  const context = await browser.newContext({ viewport: { width, height: width < 500 ? 844 : 1000 }, deviceScaleFactor: 1 });
  const page = await context.newPage(); track(page);
  let release; const barrier = new Promise(resolve => { release = resolve; });
  const requestSeen = new Promise(resolve => page.once('request', () => resolve()));
  await page.route('**/api/series?**', async route => { await barrier; await route.continue(); });
  try {
    await page.goto(`${base}/${metric ? '?metric=' + metric : ''}`, { waitUntil: 'domcontentloaded' });
    await requestSeen;
    await page.waitForFunction(() => document.querySelector('#workspace:not([hidden])'));
    assert.equal(await page.locator('.oa-guide').count(), 0, 'not shown before initial data succeeds');
    release();
    await page.waitForFunction(() => document.querySelector('.oa-guide[data-step="1"]') || document.querySelector('#chartError:not([hidden])'));
    assert.equal(await page.locator('.oa-guide[data-step="1"]').count(), 1, await page.locator('#chartError').textContent());
    const original = await snapshot(page);
    await layout(page, `${label}-first`); await shot(page, `${label}-first`);
    assert.equal(await page.evaluate(key => localStorage.getItem(key), key), null, 'start does not write completed time');
    await page.evaluate(() => { document.dispatchEvent(new CustomEvent('openalex:ready')); document.dispatchEvent(new CustomEvent('openalex:ready')); });
    assert.equal(await page.locator('.oa-guide').count(), 1);
    await page.locator('.oa-guide-next').click(); await page.locator('.oa-guide-next').click();
    await page.locator('.oa-guide-back').click();
    assert.equal(await page.locator('.oa-guide').getAttribute('data-step'), '2');
    for (let step = 2; step <= 6; step++) {
      await layout(page, `${label}-step${step}`);
      if ([2, 4, 6].includes(step)) await shot(page, `${label}-step${step}`);
      if (step < 6) await page.locator('.oa-guide-next').click();
    }
    assert.deepEqual(await snapshot(page), original, 'walking tour does not change filters, URL or data');
    if (metric === 'eci') {
      const height = await page.locator('#chart').evaluate(e => e.clientHeight);
      assert.ok(height > 4000, 'ECI uses actual tall all-country Bump');
      const heading = await page.locator('.chart-heading').boundingBox();
      const chart = await page.locator('#chart').boundingBox();
      const viewportHeight = page.viewportSize().height;
      assert.ok(heading.y >= 0 && heading.y + heading.height <= viewportHeight, 'complete chart heading remains visible');
      assert.ok(chart.y >= heading.y + heading.height - 1 && chart.y - (heading.y + heading.height) < 240 && chart.y < viewportHeight, 'viewport contains the chart beginning, not the giant SVG midpoint');
      report.checks.push({ name: `${label}-tall-bump`, chart_height: height, heading_y: heading.y, heading_bottom: heading.y + heading.height, chart_top: chart.y });
    }
    await page.keyboard.press('Escape');
    assert.equal(await page.locator('.oa-guide').count(), 0);
    assert.equal(await page.locator('.oa-guide-help').evaluate(e => e === document.activeElement), true, 'focus restored to help');
    if (!metric) await shot(page, `${label}-help-entry`);
    const afterEscape = await page.evaluate(key => localStorage.getItem(key), key);
    assert.ok(JSON.parse(afterEscape).dismissedAt > 0);
    await page.evaluate(() => document.dispatchEvent(new CustomEvent('openalex:ready')));
    assert.equal(await page.locator('.oa-guide').count(), 0, 'repeated ready cannot reopen');
    await page.reload({ waitUntil: 'networkidle' });
    assert.equal(await page.locator('.oa-guide').count(), 0, 'cooldown survives reload');
    assert.equal(await page.evaluate(key => localStorage.getItem(key), key), afterEscape, 'ordinary visits never refresh deadline');
    await page.locator('.oa-guide-help').click();
    await page.locator('.oa-guide-skip').click();
    assert.equal(await page.locator('.oa-guide').count(), 0);
    await page.locator('.oa-guide-help').click();
    for (let step = 1; step <= 6; step++) await page.locator('.oa-guide-next').click();
    assert.equal(await page.locator('.oa-guide').count(), 0, 'manual replay can complete');
    assert.deepEqual(await snapshot(page), original);
    await page.evaluate(({ key, week }) => localStorage.setItem(key, JSON.stringify({ version: 1, dismissedAt: Date.now() - week })), { key, week });
    await page.reload({ waitUntil: 'networkidle' });
    await page.locator('.oa-guide').waitFor();
    await page.locator('.oa-guide-close').click();
    report.checks.push({ name: `${label}-focus-escape-skip-finish-manual-replay-7day`, pass: true });
  } finally { release(); await context.close(); }
}
try {
  report.assets = {};
  for (const file of ['onboarding.js', 'onboarding.css']) {
    const expected = await fs.readFile(new URL(`../internal/web/static/${file}`, import.meta.url));
    const response = await browser.newPage();
    try {
      const served = await response.request.get(`${base}/static/${file}`);
      assert.equal(served.status(), 200, `${file} served without 404`);
      const digest = data => createHash('sha256').update(data).digest('hex');
      report.assets[file] = { source_sha256: digest(expected), served_sha256: digest(await served.body()) };
    } finally { await response.close(); }
  }
  await exercise(1440);
  await exercise(390);
  await exercise(1440, 'eci');
  await exercise(390, 'eci');
  const denied = await browser.newContext({ viewport: { width: 390, height: 844 } });
  await denied.addInitScript(() => Object.defineProperty(window, 'localStorage', { get() { throw new DOMException('Storage disabled', 'SecurityError'); } }));
  const page = await denied.newPage(); track(page);
  await page.goto(base, { waitUntil: 'networkidle' }); await page.locator('.oa-guide').waitFor();
  await page.keyboard.press('Escape'); await page.evaluate(() => document.dispatchEvent(new CustomEvent('openalex:ready')));
  assert.equal(await page.locator('.oa-guide').count(), 0);
  await page.locator('.oa-guide-help').click(); await page.locator('.oa-guide-skip').click();
  report.checks.push({ name: 'storage-denied-still-usable-no-repeat', pass: true }); await denied.close();
  const failed = await browser.newContext(); const failedPage = await failed.newPage();
  await failedPage.route('**/api/meta?**', route => route.abort('failed'));
  await failedPage.goto(base, { waitUntil: 'networkidle' }); await failedPage.locator('#notReady:not([hidden])').waitFor();
  assert.equal(await failedPage.locator('.oa-guide').count(), 0);
  report.checks.push({ name: 'failed-initial-load-does-not-start-guide', pass: true }); await failed.close();
  assert.deepEqual(report.errors, []); assert.deepEqual(report.outside_requests, []);
  for (const [name, asset] of Object.entries(report.assets)) assert.equal(asset.served_sha256, asset.source_sha256, `${name} matches tested source`);
  report.ok = true;
} catch (error) { report.ok = false; report.failure = error.stack; throw error; }
finally { await fs.writeFile(path.join(destination, 'report.json'), JSON.stringify(report, null, 2) + '\n'); await browser.close(); }
console.log(JSON.stringify({ ok: report.ok, checks: report.checks.length, screenshots: report.screenshots.length, destination }));
