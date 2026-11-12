// Pure contract tests: node --test scripts/bump-chart-test.mjs
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import test from 'node:test';

const sandbox = vm.createContext({Intl});
vm.runInContext(fs.readFileSync(new URL('../internal/web/static/bump.js', import.meta.url), 'utf8'), sandbox);
const bump = sandbox.OpenAlexBump;
const plain = value => JSON.parse(JSON.stringify(value));
const point = (year, value, rank, extra = {}) => ({year, value, rank, rank_universe: 180, status: 'ok', partial: false, ...extra});
const fixture = () => ({query: {metric: 'eci', start: 2023, end: 2026}, series: [
  {id: 'CN', name: '中国', points: [point(2023, 1.2, 7), point(2024, 1.4, 5), point(2026, 1.1, 9, {partial: true})]},
  {id: 'US', name: '美国', points: [point(2023, 1.8, 2), point(2024, 1.4, 5), point(2025, null, null, {status: 'model_not_available'}), point(2026, 1.7, 2, {partial: true})]},
  {id: 'JP', name: '日本', points: [point(2023, 1.2, 7), point(2024, 1.3, 7), point(2025, 1.5, 4), point(2026, null, null, {status: 'inactive', partial: true})]}
]});
const colors = {CN: '#bd5938', US: '#315f7b', JP: '#4c806d'};
const options = {color: id => colors[id], format: value => value.toFixed(4)};

test('global ranks survive selection and retain ties and server competition gaps', () => {
  const response = fixture(), all = bump.buildOption(response, options);
  const one = bump.buildOption({...response, series: [response.series[0]]}, options);
  assert.deepEqual(plain(one.series[0].data), plain(all.series[0].data));
  assert.deepEqual(plain(all.series.map(s => s.data[1])), [5, 5, 7]);
  assert.deepEqual(plain(all.series.map(s => s.id)), ['CN', 'US', 'JP']);
  assert.equal(all.series[0].data[0], 7);
  assert.equal(bump.tooltipRows(response, 0)[0].rankUniverse, 180);
});

test('missing years, unavailable ranks, invalid models and invalid scores stay null', () => {
  const response = fixture();
  response.series[0].points.push(point(2025, 1.2, 6, {status: 'disconnected'}));
  const option = bump.buildOption(response);
  assert.deepEqual(plain(option.series[0].data), [7, 5, null, 9]);
  assert.equal(option.series[1].data[2], null);
  const invalid = [point(2023, 1, null), point(2024, NaN, 3), point(2025, 1, 181), point(2026, 1, 0)];
  const empty = bump.buildOption({...response, series: [{id: 'x', points: invalid}]});
  assert.deepEqual(plain(empty.series[0].data), [null, null, null, null]);
  assert.equal(empty.graphic.length, 1);
  assert.equal(empty.series[0].endLabel.show, false);
});

test('rank is an inverse integer axis with visible dots and nonanimated gap lines', () => {
  const option = bump.buildOption(fixture(), options);
  assert.equal(option.yAxis.inverse, true);
  assert.equal(option.yAxis.min, 1);
  assert.equal(option.yAxis.minInterval, 1);
  assert.ok(option.yAxis.max >= 9 && option.yAxis.max < 180);
  assert.equal(option.animation, false);
  for (const series of option.series) {
    assert.equal(series.animation, false);
    assert.equal(series.showSymbol, true);
    assert.equal(series.showAllSymbol, true);
    assert.equal(series.symbol, 'circle');
    assert.equal(series.symbolSize, 4);
    assert.equal(series.connectNulls, false);
    assert.equal(series.smooth, false);
    assert.equal(series.itemStyle.color, colors[series.id]);
    assert.ok(series.data.every(v => typeof v === 'number' || v === null));
  }
});

test('tooltip sorts current-year ranks, keeps original scores, and marks partial years', () => {
  const response = fixture(), option = bump.buildOption(response, options);
  assert.deepEqual(plain(bump.tooltipRows(response, 0).map(r => r.id)), ['US', 'CN', 'JP']);
  assert.deepEqual(plain(bump.tooltipRows(response, 1).map(r => r.rank)), [5, 5, 7]);
  const html = option.tooltip.formatter([{dataIndex: 3}]);
  assert.ok(html.indexOf('data-tooltip-series="US"') < html.indexOf('data-tooltip-series="CN"'));
  assert.match(html, /data-tooltip-series="CN" data-tooltip-value="1.1" data-tooltip-year="2026" data-tooltip-rank="9" data-tooltip-universe="180"/);
  assert.match(html, /第 9 \/ 180/);
  assert.match(html, /1\.1000/);
  assert.match(html, /未完整年度/);
  assert.equal(option.tooltip.formatter([]), '');
  assert.deepEqual(plain(bump.tooltipRows(response, -1)), []);
});

test('formatting is escaped, response stays immutable, localized labels are supported', () => {
  const response = fixture();
  const freeze = object => {Object.freeze(object); Object.values(object).forEach(v => {if (v && typeof v === 'object') freeze(v);});};
  freeze(response);
  const snapshot = JSON.stringify(response);
  const option = bump.buildOption(response, {color: () => '"><img src=x>', format: () => '<b>score</b>', displayName: s => `${s.id}<script>`});
  assert.equal(option.series[0].name, 'CN<script>');
  assert.equal(option.series[0].itemStyle.color, '#315f7b');
  const html = option.tooltip.formatter([{dataIndex: 0}]);
  assert.match(html, /CN&lt;script&gt;/);
  assert.match(html, /&lt;b&gt;score&lt;\/b&gt;/);
  assert.doesNotMatch(html, /<script>|<img|<b>/);
  assert.equal(JSON.stringify(response), snapshot);
});

test('PCI describes a full discipline universe; mobile and inferred years remain usable', () => {
  const response = fixture();
  response.query.metric = 'pci';
  assert.match(bump.definition(response), /同层全部有效学科/);
  assert.match(bump.definition(response), /选择不改变全局排名/);
  const option = bump.buildOption(response, {mobile: true});
  assert.ok(option.grid.right < bump.buildOption(response).grid.right);
  assert.ok(option.series[0].endLabel.fontSize >= 12);
  assert.deepEqual(plain(bump.years({series: [response.series[0]]})), [2023, 2024, 2026]);
  assert.deepEqual(plain(bump.years({})), []);
});
