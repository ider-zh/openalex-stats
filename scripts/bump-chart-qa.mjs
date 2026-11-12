// Synthetic component QA using the same embedded ECharts build as production.
// node scripts/bump-chart-qa.mjs (optional dependency: browser-qa.mjs documents it)
// Full live-data/corpus integration acceptance is scripts/browser-qa.mjs.
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import {createRequire} from 'node:module';
const require = createRequire(import.meta.url);
const {chromium} = require('../bin/browser-qa/node_modules/playwright');
const output = path.resolve(process.env.BUMP_QA_OUTPUT || 'research/bump-chart-qa');
await fs.mkdir(output, {recursive: true});
const ids = ['CN', 'US', 'GB', 'DE', 'FR', 'JP', 'IN', 'CA', 'KR'];
const names = ['中国', '美国', '英国', '德国', '法国', '日本', '印度', '加拿大', '韩国'];
const palette = ['#bd5938','#315f7b','#4c806d','#ad8a37','#806b91','#738d9b','#a9586c','#5c7354','#9d795d'];
const fixture = {query: {metric: 'eci', start: 2000, end: 2026}, series: ids.map((id, c) => ({id, name: names[c], points: Array.from({length: 27}, (_, i) => {
  const rank = 2 + Math.round(c * 2.2 + Math.sin(i / 4 + c) * 2.5 + (c === 0 ? (26-i)/4 : 0));
  return {year: 2000+i, value: 2.6-rank*.09, rank, rank_universe: 180-i%5, status: 'ok', partial: i === 26};
}).filter(p => !(id === 'JP' && p.year === 2010))}))};
const report = {fixture: 'synthetic server-assigned global ranks; not research results', echarts: null, checks: [], screenshots: []};
const browser = await chromium.launch({channel: 'chrome', headless: true});
try {
  for (const [name, width, height] of [['desktop', 1440, 900], ['mobile', 390, 844]]) {
    const page = await browser.newPage({viewport: {width, height}});
    const errors = [];
    page.on('pageerror', e => errors.push(e.message));
    await page.setContent(`<!doctype html><html lang="zh-CN"><meta charset="utf-8"><title>Bump QA</title><style>body{margin:0;background:#f4f4ed;color:#162d39;font-family:'Trebuchet MS','Microsoft YaHei',sans-serif}.wrap{margin:32px auto;padding:24px;max-width:1250px;background:#fffefa;border:1px solid #d7dcd8}h1{font-size:23px;margin:0 0 8px}p{font-size:12px;color:#718087;line-height:1.7}#chart{height:540px}small{color:#87938f}@media(max-width:600px){.wrap{margin:12px 8px;padding:16px 8px}h1{font-size:19px}#chart{height:520px}}</style><div class="wrap"><small>OPENALEX · COMPONENT QA · SYNTHETIC DATA</small><h1>知识复杂度 · 全球排名变迁</h1><p id="description"></p><div id="chart"></div><p>每个圆点代表一年；悬浮查看原始分数。2026* 为未完整年度。</p></div></html>`);
    await page.addScriptTag({path: path.resolve('internal/web/static/vendor/echarts.min.js')});
    await page.addScriptTag({path: path.resolve('internal/web/static/bump.js')});
    await page.evaluate(({fixture, ids, palette, mobile}) => {
      window.fixture = fixture;
      window.chart = echarts.init(document.getElementById('chart'));
      window.chart.setOption(OpenAlexBump.buildOption(fixture, {mobile, color: id => palette[ids.indexOf(id)]}), true);
      chart.getZr().flush();
      document.getElementById('description').textContent = OpenAlexBump.definition(fixture);
    }, {fixture, ids, palette, mobile: name === 'mobile'});
    const geometry = await page.evaluate(() => {
      const plotted = chart.getModel().getSeries().map(model => {
        const values = chart.getOption().series[model.seriesIndex].data;
        const polyline = chart.getViewOfSeriesModel(model)._polyline.shape.points;
        const data = model.getData();
        return {id: model.id, points: values.map((rank, i) => ({rank, actual: Array.from(polyline.slice(i*2, i*2+2)), expected: rank === null ? null : chart.convertToPixel({seriesIndex: model.seriesIndex}, [i, rank]), dot: Boolean(data.getItemGraphicEl(i))}))};
      });
      return {version: echarts.version, plotted, top: chart.convertToPixel({yAxisIndex:0}, 1), bottom: chart.convertToPixel({yAxisIndex:0}, 20), overflow: document.documentElement.scrollWidth > innerWidth};
    });
    report.echarts = geometry.version;
    assert.equal(geometry.overflow, false);
    assert.ok(geometry.top < geometry.bottom, 'rank 1 must render above rank 20');
    for (const series of geometry.plotted) for (const point of series.points) {
      if (point.rank === null) continue;
      assert.ok(Math.abs(point.actual[0]-point.expected[0]) < .1 && Math.abs(point.actual[1]-point.expected[1]) < .1, `${series.id} path must reach its global rank`);
      assert.equal(point.dot, true, `${series.id} every valid year has a dot`);
    }
    const jp = geometry.plotted.find(s => s.id === 'JP');
    assert.equal(jp.points[10].rank, null);
    assert.equal(jp.points[10].expected, null);
    const box = await page.locator('#chart').boundingBox();
    const target = geometry.plotted[0].points[20].expected;
    await page.mouse.move(box.x+target[0], box.y+target[1]);
    const hovered = page.locator('[data-tooltip-series="CN"]');
    await hovered.waitFor({state: 'visible'});
    assert.equal(await hovered.getAttribute('data-tooltip-rank'), String(fixture.series[0].points.find(p => p.year === 2020).rank));
    assert.equal(await hovered.getAttribute('data-tooltip-value'), String(fixture.series[0].points.find(p => p.year === 2020).value));
    const ranks = await page.locator('[data-tooltip-rank]').evaluateAll(rows => rows.map(row => Number(row.dataset.tooltipRank)));
    assert.deepEqual(ranks, [...ranks].sort((a,b)=>a-b));
    assert.deepEqual(errors, []);
    const screenshot = path.join(output, `${name}.png`);
    await page.screenshot({path: screenshot, fullPage: true});
    report.screenshots.push(screenshot);
    report.checks.push({viewport: name, width, valid_points: geometry.plotted.flatMap(s=>s.points).filter(p=>p.rank!==null).length, pass: true, checked: ['rank path coordinates', 'rank1 above rank20', 'all annual dots', 'missing-year gap', 'actual hover score/rank and sort', 'no page errors or horizontal overflow']});
    await page.close();
  }
} finally {await browser.close();}
await fs.writeFile(path.join(output, 'report.json'), JSON.stringify(report, null, 2));
console.log(JSON.stringify(report, null, 2));
