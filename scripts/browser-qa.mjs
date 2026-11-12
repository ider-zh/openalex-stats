// Development-only browser acceptance. Statistics and service remain Go.
// Install the pinned optional SDK with:
// npm --prefix bin/browser-qa install --save-exact playwright@1.56.1
import { createRequire } from 'node:module';
import fs from 'node:fs/promises';
import path from 'node:path';
import assert from 'node:assert/strict';

const require = createRequire(import.meta.url);
const { chromium } = require('../bin/browser-qa/node_modules/playwright');
const base = process.env.QA_URL || 'http://192.168.1.229:8899';
const mode = process.env.QA_MODE || 'pilot';
const requestedCorpus = process.env.QA_CORPUS || '';
assert.ok(!requestedCorpus || ['core','all'].includes(requestedCorpus),'QA_CORPUS must be core or all');
const destination = path.resolve(process.env.QA_OUTPUT || `research/browser-${mode}`);
const assetOverride = process.env.QA_ASSET_DIR ? path.resolve(process.env.QA_ASSET_DIR) : null;
await fs.mkdir(destination, { recursive: true });
const report = { mode, url: base, started_at: new Date().toISOString(), checks: [], errors: [], requests_outside_service: [], screenshots: [] };
if (assetOverride) report.asset_override = assetOverride;
const browser = await chromium.launch({ channel: 'chrome', headless: true });
try {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1100 }, deviceScaleFactor: 1 });
  const page = await context.newPage();
  await context.addInitScript(()=>localStorage.setItem('openalex-atlas:onboarding:v1',JSON.stringify({version:1,dismissedAt:Date.now()})));
  // Optional developer-only overlay verifies an embedded asset change against
  // the live real-data API without restarting its service. Final deployed QA
  // must be repeated without this flag; the report always records the overlay.
  if (assetOverride) {
    await page.route(url=>url.origin===new URL(base).origin&&url.pathname==='/',route=>route.fulfill({path:path.join(assetOverride,'index.html'),contentType:'text/html'}));
    await page.route('**/static/app.js', route => route.fulfill({ path: path.join(assetOverride, 'app.js'), contentType: 'text/javascript' }));
    await page.route('**/static/app.css', route => route.fulfill({ path: path.join(assetOverride, 'app.css'), contentType: 'text/css' }));
  }
  page.on('pageerror', error => report.errors.push(error.message));
  page.on('request', request => {
    const url = request.url();
    if (/^https?:/.test(url) && new URL(url).origin !== new URL(base).origin) report.requests_outside_service.push(url);
  });
  const check = (name, detail) => report.checks.push({ name, pass: true, detail });
  const chooseDiscipline=async id=>{await page.locator('#disciplineSummary').click();await page.locator('#clearDisciplines').click();await page.locator('#disciplineSearch').fill(id);await page.locator(`[data-discipline-id="${id}"]:not(:disabled)`).first().click();await page.locator('#disciplineSummary').click();};
  const chooseScenario=async id=>{const target=(await page.evaluate(async()=> {const corpus=new URLSearchParams(location.search).get('corpus');return (await (await fetch(`/api/meta${corpus?`?corpus=${corpus}`:''}`)).json()).scenarios;})).find(s=>s.id===id);assert.ok(target);await page.locator(`[data-country-mode="${target.country_mode}"]`).click();if((await page.locator('#scenario').inputValue())!==id)await page.locator(`[data-threshold="${target.concept_threshold}"]`).click();};
  const chartSeries = () => page.evaluate(() => window.echarts.getInstanceByDom(document.getElementById('chart')).getOption().series);
  const renderedState = () => page.evaluate(() => {
    const chart = window.echarts.getInstanceByDom(document.getElementById('chart'));
    return {
      finished: chart.getZr().animation.isFinished(),
      size: [chart.getWidth(), chart.getHeight()],
      table: [...document.querySelectorAll('#comparisonBody tr')].map(row => ({ id: row.dataset.seriesId, year: row.dataset.year, value: row.dataset.value, text: row.cells[1].textContent })),
      series: chart.getModel().getSeries().map(model => {
        const line = chart.getOption().series[model.seriesIndex];
        const last = line.data.findLastIndex(value => value != null);
        const points = chart.getViewOfSeriesModel(model)._polyline?.shape?.points;
        return { name: model.name, last_value: last >= 0 ? line.data[last] : null, expected_pixel: last >= 0 ? chart.convertToPixel({ seriesIndex: model.seriesIndex }, [last, line.data[last]]) : null, rendered_pixel: last >= 0 && points ? Array.from(points.slice(last * 2, last * 2 + 2)) : null };
      })
    };
  });
  const settlePlot = async body => {
    await page.waitForFunction(expected => {
      const element = document.getElementById('chart');
      const chart = window.echarts?.getInstanceByDom(element);
      if (!chart || !chart.getZr().animation.isFinished() || chart.getWidth() !== element.clientWidth || chart.getHeight() !== element.clientHeight) return false;
      const option = chart.getOption(), models = chart.getModel().getSeries();
      const rankView=['eci','pci'].includes(expected.query.metric)&&document.querySelector('[data-model-view="rank"]').getAttribute('aria-pressed')==='true';
      if (models.length !== expected.series.length) return false;
      // getOption validates the target data. These checks additionally validate
      // the actual drawn path, which can still be interpolating an old curve.
      for (let i = 0; i < models.length; i++) {
        const points = chart.getViewOfSeriesModel(models[i])._polyline?.shape?.points;
        for (let j = 0; j < expected.series[i].points.length; j++) {
          const raw = expected.series[i].points[j].value;
          const value = rankView?(expected.series[i].points[j].rank??null):raw == null ? null : expected.query.metric === 'share' ? raw * 100 : raw;
          if (option.series[i].data[j] !== value) return false;
          if (value == null) continue;
          const pixel = chart.convertToPixel({ seriesIndex: i }, [j, value]);
          if (!points || !Number.isFinite(points[j * 2]) || !Number.isFinite(points[j * 2 + 1]) || Math.abs(points[j * 2] - pixel[0]) > .75 || Math.abs(points[j * 2 + 1] - pixel[1]) > .75) return false;
        }
      }
      const rows = [...document.querySelectorAll('#comparisonBody tr')];
      if (rows.length !== expected.series.length || document.getElementById('tableYear').textContent !== String(expected.query.end)) return false;
      const formatter = new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 2 });
      for (const series of expected.series) {
        const row = rows.find(row => row.dataset.seriesId === series.id);
        const point = series.points.at(-1);
        if (!row || row.dataset.year !== String(point.year) || row.dataset.value !== (point.value == null ? '' : String(point.value))) return false;
        if(rankView&&row.dataset.rank!==(point.rank==null?'':String(point.rank)))return false;
        const text = rankView?(point.rank==null?'—':`#${point.rank} / ${point.rank_universe}`):point.value == null ? '—' : expected.query.metric === 'share' ? `${formatter.format(point.value * 100)}%` : formatter.format(point.value);
        if (row.cells[1].textContent !== text) return false;
      }
      return true;
    }, body);
    // Allow the completed, verified canvas frame to reach the compositor.
    await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
  };
  const act = async (action, predicate) => {
    const response = page.waitForResponse(response => {
      if (!response.url().includes('/api/series?')) return false;
      const p = new URL(response.url()).searchParams;
      return response.status() === 200 && predicate(p);
    });
    response.catch(()=>{});
    await action();
    const body = await (await response).json();
    await page.waitForFunction(expected => {
      const element = document.getElementById('chart');
      const series = window.echarts?.getInstanceByDom(element)?.getOption()?.series;
      const rankView=['eci','pci'].includes(expected.query.metric)&&document.querySelector('[data-model-view="rank"]').getAttribute('aria-pressed')==='true';
      if (!series || series.length !== expected.series.length || !document.getElementById('chartError').hidden) return false;
      const names=new Intl.DisplayNames(['zh-CN'],{type:'region'});
      return series.every((line,i) => line.name === (expected.series[i].country==='GLOBAL'?`${expected.series[i].discipline_name} · 全球 PCI`:names.of(expected.series[i].country||expected.series[i].id)) && line.data.length === expected.series[i].points.length && line.data.every((value,j) => {
        const point = expected.series[i].points[j].value;
        return value === (rankView?(expected.series[i].points[j].rank??null):point == null ? null : expected.query.metric === 'share' ? point * 100 : point);
      }));
    }, body);
    await settlePlot(body);
    return body;
  };
  const entry=new URL(base);if(requestedCorpus)entry.searchParams.set('corpus',requestedCorpus);
  await page.goto(entry.toString(), { waitUntil: 'networkidle' });
  await page.locator('#workspace').waitFor({ state: 'visible' });
  await page.waitForFunction(() => document.querySelector('#chart svg, #chart canvas') && document.getElementById('chartDefinition').textContent.length > 0);
  const meta = await page.evaluate(async () => {const corpus=new URLSearchParams(location.search).get('corpus');return (await fetch(`/api/meta${corpus?`?corpus=${corpus}`:''}`)).json();});
  const corpus=meta.corpus||meta.scope?.corpus||'core';
  if(requestedCorpus)assert.equal(corpus,requestedCorpus);
  assert.equal(await page.locator('#corpus').inputValue(),corpus);
  const mainScenario = meta.scenarios.find(s => s.id === meta.eci_scenario_id);
  assert.ok(mainScenario);assert.equal(mainScenario.country_mode, 'fractional');
  if(process.env.QA_EXPECT_CONCEPT_MODE) assert.equal(mainScenario.concept_mode,process.env.QA_EXPECT_CONCEPT_MODE);
  const wholeScenario = meta.scenarios.find(s => s.country_mode === 'whole' && s.concept_mode === mainScenario.concept_mode && s.concept_threshold === mainScenario.concept_threshold);
  assert.ok(wholeScenario);
  report.dataset = { corpus, status: meta.status, selected_works: meta.selected_works, snapshot_date: meta.snapshot_date, scenario: meta.eci_scenario_id };
  assert.equal(meta.ready, true);
  if (mode === 'formal') assert.equal(meta.status, 'full_snapshot_complete');
  assert.equal(await page.locator('#yearStart').inputValue(), '2000');
  assert.equal(await page.locator('#yearEnd').inputValue(), '2025');
  assert.deepEqual(await page.locator('#level option').evaluateAll(options => options.map(o => o.value)), ['field', 'subfield']);
  assert.equal(await page.locator('#scenario').inputValue(), mainScenario.id);
  assert.ok((await page.locator('#scenarioHint').textContent()).includes('Concepts 阈值不适用'));
  check('real_dataset_and_defaults', report.dataset);
  const initialBody = await page.evaluate(async () => (await fetch(`/api/series${location.search}`)).json());
  await settlePlot(initialBody);
  check('desktop_topics_field_rendered_paths_and_table_match_api', await renderedState());
  const expectedDefaults=['CN','US','GB','DE','FR','JP','IN','CA','KR'].filter(id=>meta.countries.some(c=>c.id===id));
  assert.deepEqual(initialBody.query.countries,expectedDefaults);
  check('default_nine_common_research_countries',initialBody.query.countries);
  const symbols=await page.evaluate(()=>{const chart=window.echarts.getInstanceByDom(document.getElementById('chart'));return chart.getModel().getSeries().map((series,i)=>({show:chart.getOption().series[i].showSymbol,drawn:Array.from({length:series.getData().count()},(_,j)=>series.getData().getItemGraphicEl(j)).filter(Boolean).length}));});
  assert.ok(symbols.every(s=>s.show&&s.drawn>0));check('annual_points_visibly_drawn',symbols);
  await page.evaluate(()=>window.echarts.getInstanceByDom(document.getElementById('chart')).dispatchAction({type:'showTip',seriesIndex:0,dataIndex:25}));
  await page.waitForFunction(()=>document.querySelectorAll('[data-tooltip-series]').length>0);
  const tooltip=await page.locator('[data-tooltip-series]').evaluateAll(rows=>rows.map(r=>({id:r.dataset.tooltipSeries,value:r.dataset.tooltipValue===''?null:Number(r.dataset.tooltipValue)})));
  const expectedTooltip=initialBody.series.map((s,order)=>({id:s.id,value:s.points.at(-1).value,order})).sort((a,b)=>a.value==null?(b.value==null?a.order-b.order:1):b.value==null?-1:b.value-a.value||a.order-b.order).map(({id,value})=>({id,value}));
  assert.deepEqual(tooltip,expectedTooltip);check('hover_tooltip_value_descending_null_last',tooltip);
  await page.evaluate(()=>window.echarts.getInstanceByDom(document.getElementById('chart')).dispatchAction({type:'hideTip'}));
  const initial = path.join(destination, 'topics-field.png');
  await page.screenshot({ path: initial, fullPage: true, animations: 'disabled' }); report.screenshots.push(initial);

  for(const search of ['澳大利亚','Australia','AU']){await page.locator('#countrySearch').fill(search);assert.equal(await page.locator('#countryList input[value="AU"]').count(),1);}
  await act(()=>page.locator('#selectCountryResults').click(),p=>p.get('countries')?.split(',').includes('AU'));
  await act(()=>page.locator('#resetCountries').click(),p=>p.get('countries')===expectedDefaults.join(','));
  await page.locator('#clearCountries').click();await page.locator('#chartError').waitFor({state:'visible'});
  await act(()=>page.locator('#resetCountries').click(),p=>p.get('countries')===expectedDefaults.join(','));
  await page.locator('#countrySearch').fill('');check('chinese_english_iso_search_bulk_clear_reset',null);
  const childLayer=meta.layers.find(l=>l.scheme==='topics'&&l.level==='subfield');
  const parented=childLayer.labels.find(label=>label.parent_ids?.length);
  if(mainScenario.concept_mode==='whole')assert.ok(parented,'new formal dictionary must expose actual Topics parents');
  const child=parented||childLayer.labels[0];
  await act(()=>page.locator('[data-level="subfield"]').click(),p=>p.get('level')==='subfield');
  const treeBody=await act(()=>chooseDiscipline(child.id),p=>p.get('discipline')===child.id&&p.get('level')==='subfield');
  if(parented)assert.ok(await page.locator(`[data-parent-id="${child.parent_ids[0]}"] [data-discipline-id="${child.id}"]`).count());
  else assert.ok((await page.locator('#hierarchyHint').textContent()).includes('缺少'));
  assert.equal(treeBody.query.discipline,child.id);check('searchable_tree_uses_real_parent_or_explicit_missing', {id:child.id,parents:child.parent_ids||[]});
  await act(()=>page.locator('[data-level="field"]').click(),p=>p.get('level')==='field');

  await act(() => page.locator('[data-scheme="concepts"]').click(), p => p.get('scheme') === 'concepts' && p.get('level') === '0');
  assert.deepEqual(await page.locator('#level option').evaluateAll(options => options.map(o => o.value)), ['0', '1']);
  const biology = 'https://openalex.org/C86803240';
  assert.ok(await page.locator(`#discipline option[value="${biology}"]`).count());
  const biologyPublications = await act(() => chooseDiscipline(biology), p => p.get('discipline') === biology && p.get('metric') === 'publications');
  assert.ok(biologyPublications.series.length >= 2);
  assert.ok(biologyPublications.series.every(s => s.points.length === 26));
  check('concept_biology_multi_country_real_series', { countries: biologyPublications.series.map(s => s.id), points: 26 });
  if(meta.scenarios.some(s=>s.country_mode===mainScenario.country_mode&&s.concept_mode===mainScenario.concept_mode&&s.concept_threshold===.5)){
    await act(()=>page.locator('[data-threshold="0.5"]').click(),p=>p.get('scenario')!==mainScenario.id);
    await act(()=>page.locator(`[data-threshold="${mainScenario.concept_threshold}"]`).click(),p=>p.get('scenario')===mainScenario.id);
    check('concept_threshold_segmented_one_click',null);
  }
  if(mainScenario.concept_mode==='whole'){
    assert.equal(biologyPublications.counting.concept_mode,'whole');
    assert.equal(biologyPublications.counting.country_mode,'fractional');
    assert.equal(biologyPublications.counting.publication_counts_integer,false);
    assert.ok((await page.locator('#scenarioHint').textContent()).includes('命中一个标签即记 1'));
    assert.ok((await page.locator('#scenarioHint').textContent()).includes('仍可为小数'));
    check('concept_hit_whole_country_fractional_explicit',biologyPublications.counting);
    const wholePub=await act(()=>chooseScenario(wholeScenario.id),p=>p.get('scenario')===wholeScenario.id);
    assert.equal(wholePub.counting.publication_counts_integer,true);
    assert.ok(wholePub.definition.includes('整数计数'));
    assert.ok(wholePub.series.some(s=>s.points.some(p=>p.value>0)));
    for(const s of wholePub.series)for(const p of s.points)assert.ok(Number.isInteger(p.value),`whole Biology publication must be integer: ${s.id}/${p.year}=${p.value}`);
    const wholeCit=await act(()=>page.locator('[data-metric="citations"]').click(),p=>p.get('metric')==='citations'&&p.get('scenario')===wholeScenario.id);
    for(const s of wholeCit.series)for(const p of s.points)assert.ok(Number.isInteger(p.value),`whole Biology citation must be integer: ${s.id}/${p.year}=${p.value}`);
    check('concept_biology_whole_publications_and_citations_integer',{scenario:wholeScenario.id,publication_points:wholePub.series.reduce((n,s)=>n+s.points.length,0),citation_points:wholeCit.series.reduce((n,s)=>n+s.points.length,0)});
    await act(()=>page.locator('[data-metric="publications"]').click(),p=>p.get('metric')==='publications');
    await act(()=>chooseScenario(mainScenario.id),p=>p.get('scenario')===mainScenario.id);
  }
  const cited = await act(() => page.locator('[data-metric="citations"]').click(), p => p.get('metric') === 'citations');
  assert.ok(cited.definition.includes('引用') || cited.definition.includes('被引'));
  check('annual_received_citations', { series: cited.series.length, definition: cited.definition });
  const biologyScreenshot = path.join(destination, 'biology-citations.png');
  await page.screenshot({ path: biologyScreenshot, fullPage: true, animations: 'disabled' }); report.screenshots.push(biologyScreenshot);

  const eci = await act(() => page.locator('[data-metric="eci"]').click(), p => p.get('metric') === 'eci');
  assert.ok(eci.series.every(s=>s.points.every(p=>'rank' in p&&'rank_universe' in p)));
  assert.equal(await page.evaluate(()=>window.echarts.getInstanceByDom(document.getElementById('chart')).getOption().yAxis[0].inverse),true);
  await page.locator('[data-model-view="score"]').click();await settlePlot(eci);
  await page.locator('[data-model-view="rank"]').click();await settlePlot(eci);
  const singleRank=await page.evaluate(async id=>{const p=new URLSearchParams(location.search);p.set('countries',id);return (await fetch(`/api/series?${p}`)).json();},eci.series[0].id);
  assert.deepEqual(singleRank.series[0].points,eci.series[0].points);
  check('global_bump_ranks_selection_independent_and_score_toggle',null);
  assert.equal(await page.locator('#discipline').isDisabled(), true);
  assert.ok(eci.series.length >= 2);
  const alt = meta.layers.find(l => l.scheme === 'concepts' && l.level === '0').labels.find(l => l.id !== biology).id;
  const unchanged = await page.evaluate(async alternative => {
    const params = new URLSearchParams(location.search); params.set('discipline', alternative);
    return (await fetch(`/api/series?${params}`)).json();
  }, alt);
  assert.deepEqual(eci.series, unchanged.series);
  for (const series of eci.series) for (const point of series.points) {
    if (!['ok', 'unstable'].includes(point.status)) assert.equal(point.value, null);
  }
  check('country_eci_is_subject_independent_and_invalid_scores_are_gaps', { series: eci.series.length });

  const pci = await act(() => page.locator('[data-metric="pci"]').click(), p => p.get('metric') === 'pci');
  assert.equal(pci.series.length, 1);
  assert.equal((await chartSeries()).length, 1);
  check('discipline_pci_is_one_global_series', { id: pci.series[0].id });
  await act(() => page.locator('[data-metric="publications"]').click(), p => p.get('metric') === 'publications');
  await page.locator('#countrySearch').fill('AU');
  const added = await act(() => page.locator('#countryList input[value="AU"]').check(), p => p.get('countries')?.split(',').includes('AU'));
  await page.locator('#countrySearch').fill('');
  assert.ok(added.series.some(s => s.id === 'AU'));
  check('country_multiselect_updates_query', { countries: added.series.map(s => s.id) });

  const partial = await act(() => page.locator('#yearEnd').selectOption('2026'), p => p.get('end') === '2026');
  assert.ok(partial.series.every(s => s.points.at(-1).partial === true));
  assert.equal(await page.locator('#partialBadge').isVisible(), true);
  check('partial_year_explicitly_marked', { year: 2026 });
  await act(() => page.locator('#yearEnd').selectOption('2025'), p => p.get('end') === '2025');

  await act(() => page.locator('[data-metric="eci"]').click(), p => p.get('metric') === 'eci');
  const whole = await act(() => chooseScenario(wholeScenario.id), p => p.get('scenario') === wholeScenario.id);
  assert.ok(whole.series.every(s => s.points.every(p => p.value === null && p.status === 'model_not_available')));
  check('whole_scenario_does_not_borrow_fractional_eci', null);
  await act(() => chooseScenario(mainScenario.id), p => p.get('scenario') === mainScenario.id);

  // Exercise the actual UI input selector for each independent layer, rather
  // than merely issuing eight backend URLs. Each model exposes both its
  // country ECI and global discipline PCI through the same visible controls.
  for (const selection of [
    { scheme: 'topics', level: 'field', preferred: 'https://openalex.org/fields/17' },
    { scheme: 'topics', level: 'subfield' },
    { scheme: 'concepts', level: '0', preferred: biology },
    { scheme: 'concepts', level: '1', preferred: 'https://openalex.org/C54355233' }
  ]) {
    const labels = meta.layers.find(layer => layer.scheme === selection.scheme && layer.level === selection.level).labels;
    const discipline = labels.some(label => label.id === selection.preferred) ? selection.preferred : labels[0].id;
    const params = new URLSearchParams({ corpus,scenario: mainScenario.id, scheme: selection.scheme, level: selection.level, discipline, countries: added.series.map(series => series.id).join(','), metric: 'eci', basis: 'publications', start: '2000', end: '2025' });
    await page.goto(`${base}/?${params}`, { waitUntil: 'networkidle' });
    await page.locator('#workspace').waitFor({ state: 'visible' });
    const publicationECI = await page.evaluate(async () => (await fetch(`/api/series${location.search}`)).json());
    await settlePlot(publicationECI);
    assert.equal(await page.locator('#basisWrap').isVisible(), true);
    assert.equal(await page.locator('#basis').inputValue(), 'publications');
    const citationECI = await act(() => page.locator('[data-basis="citations"]').click(), p => p.get('metric') === 'eci' && p.get('basis') === 'citations' && p.get('scheme') === selection.scheme && p.get('level') === selection.level);
    const citationPCI = await act(() => page.locator('[data-metric="pci"]').click(), p => p.get('metric') === 'pci' && p.get('basis') === 'citations');
    const publicationPCI = await act(() => page.locator('[data-basis="publications"]').click(), p => p.get('metric') === 'pci' && p.get('basis') === 'publications');
    assert.equal(citationPCI.series.length, 1); assert.equal(publicationPCI.series.length, 1);
    for (const body of [publicationECI, citationECI, publicationPCI, citationPCI]) {
      assert.equal(body.query.scheme, selection.scheme); assert.equal(body.query.level, selection.level);
      assert.ok(body.series.every(series => series.points.length === 26));
      if(body.query.metric==='eci')assert.deepEqual(body.query.countries,added.series.map(s=>s.id));
      for (const series of body.series) for (const point of series.points) if (!['ok', 'unstable'].includes(point.status)) assert.equal(point.value, null);
    }
    check(`model_input_ui_${selection.scheme}_${selection.level}_publications`, { discipline, country_series: publicationECI.series.length, pci_series: publicationPCI.series.length });
    check(`model_input_ui_${selection.scheme}_${selection.level}_citations`, { discipline, country_series: citationECI.series.length, pci_series: citationPCI.series.length });
    await act(() => page.locator('[data-metric="eci"]').click(), p => p.get('metric') === 'eci' && p.get('basis') === 'publications');
  }
  await act(() => page.locator('[data-basis="citations"]').click(), p => p.get('metric') === 'eci' && p.get('basis') === 'citations');
  const partialECI = await act(() => page.locator('#yearEnd').selectOption('2026'), p => p.get('metric') === 'eci' && p.get('end') === '2026');
  const partialPCI = await act(() => page.locator('[data-metric="pci"]').click(), p => p.get('metric') === 'pci' && p.get('end') === '2026');
  assert.ok(partialECI.series.every(series => series.points.at(-1).partial));
  assert.ok(partialPCI.series.every(series => series.points.at(-1).partial));
  assert.equal(await page.locator('#partialBadge').isVisible(), true);
  check('complexity_inputs_partial_2026_marked', { eci: partialECI.query.basis, pci: partialPCI.query.basis });
  await act(() => page.locator('#yearEnd').selectOption('2025'), p => p.get('end') === '2025');
  await act(() => page.locator('[data-scheme="topics"]').click(), p => p.get('scheme') === 'topics');
  await act(() => page.locator('[data-metric="rca"]').click(), p => p.get('metric') === 'rca');
  const subfield = await act(() => page.locator('[data-level="subfield"]').click(), p => p.get('level') === 'subfield');
  assert.ok(subfield.series.length >= 2);check('topics_subfield_rca', { points: subfield.series[0].points.length });
  check('desktop_subfield_rca_rendered_paths_and_table_match_api', await renderedState());

  const downloads = await page.evaluate(async () => {
    const href = document.getElementById('downloadSeries').getAttribute('href');
    const csv = await (await fetch(href)).text();
    const corpus=new URLSearchParams(location.search).get('corpus');
    const parquet = await fetch(`/download?file=cube.parquet${corpus?`&corpus=${corpus}`:''}`, { headers: { Range: 'bytes=0-3' } });
    const signature = await parquet.text();
    const unsafe = await fetch('/download?file=..%2F..%2Fetc%2Fpasswd');
    return { header: csv.split('\n')[0], csvRows: csv.trim().split('\n').length, signature, unsafeStatus: unsafe.status };
  });
  assert.ok(downloads.header.includes('year,value,status,partial'));
  assert.equal(downloads.signature, 'PAR1');assert.ok(downloads.unsafeStatus >= 400);
  check('real_csv_parquet_download_and_path_safety', downloads);

  await page.setViewportSize({ width: 390, height: 844 });
  await page.waitForFunction(() => document.documentElement.scrollWidth <= innerWidth + 1);
  await settlePlot(subfield);
  check('mobile_rendered_paths_and_table_match_api', await renderedState());
  const mobile = path.join(destination, 'mobile.png'); await page.screenshot({ path: mobile, fullPage: true, animations: 'disabled' }); report.screenshots.push(mobile);
  check('mobile_no_horizontal_overflow', { width: 390 });
  const catalog=await page.evaluate(async()=>{const response=await fetch('/api/catalog');return response.ok?await response.json():null;});
  if(catalog){
    assert.ok(catalog.datasets.some(d=>d.id===corpus));
    if(catalog.datasets.length===2){
      const other=corpus==='core'?'all':'core';
      await page.locator(`[data-corpus="${other}"]`).click();await page.waitForURL(url=>url.searchParams.get('corpus')===other);await page.waitForLoadState('networkidle');
      const changed=await page.evaluate(async()=>({meta:await(await fetch(`/api/meta?corpus=${new URLSearchParams(location.search).get('corpus')}`)).json(),body:await(await fetch(`/api/series${location.search}`)).json(),run:await(await fetch(`/download?file=run.json&corpus=${new URLSearchParams(location.search).get('corpus')}`)).json()}));
      await settlePlot(changed.body);assert.equal(changed.meta.corpus,other);assert.equal(changed.body.query.corpus,other);assert.equal(changed.meta.scope.corpus,other);
      for(const key of ['scheme','level','discipline','metric','basis','start','end','countries'])assert.deepEqual(changed.body.query[key],subfield.query[key]);
      assert.equal(changed.meta.selected_works,catalog.datasets.find(d=>d.id===other).selected_works);assert.equal(changed.run.selected_works,changed.meta.selected_works);
      await page.locator(`[data-corpus="${corpus}"]`).click();await page.waitForURL(url=>url.searchParams.get('corpus')===corpus);await page.waitForLoadState('networkidle');
      const restored=await page.evaluate(async()=> (await fetch(`/api/series${location.search}`)).json());await settlePlot(restored);assert.deepEqual(restored.series,subfield.series);
      check('core_all_one_click_preserves_filters_and_routes_actual_dataset',{from:corpus,other,selected_works:changed.meta.selected_works});
    }else check('single_corpus_catalog_identity',{corpus});
  }
  assert.deepEqual(report.errors, []); assert.deepEqual(report.requests_outside_service, []);
  check('no_page_errors_or_runtime_external_requests', null);
  report.status = 'passed';
} catch (error) {
  report.status = 'failed'; report.failure = error.stack || String(error);
  throw error;
} finally {
  report.completed_at = new Date().toISOString();
  await fs.writeFile(path.join(destination, 'browser-qa.json'), JSON.stringify(report, null, 2));
  await browser.close();
  console.log(JSON.stringify({ status: report.status, checks: report.checks.length, output: destination, failure: report.failure }));
}
