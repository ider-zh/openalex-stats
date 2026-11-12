// Real-data acceptance for the country x discipline explorer and local flags.
import {createRequire} from 'node:module';
import fs from 'node:fs/promises';
import path from 'node:path';
import assert from 'node:assert/strict';
const require=createRequire(import.meta.url),{chromium}=require('../bin/browser-qa/node_modules/playwright');
const base=process.env.QA_URL||'http://192.168.1.229:8901';
const out=path.resolve(process.env.QA_OUTPUT||'research/ui-multi-discipline-v12/feature-candidate');
await fs.mkdir(out,{recursive:true});
const browser=await chromium.launch({channel:'chrome',headless:true});
const report={status:'running',url:base,asset_override:false,checks:[],started_at:new Date().toISOString()};
const check=(name,detail)=>report.checks.push({name,pass:true,detail});
const countries=['CN','US','GB','DE','FR','JP','IN','CA','KR'];
function csv(text){let rows=[],row=[],value='',quoted=false;for(let i=0;i<text.length;i++){const c=text[i];if(c==='"'){if(quoted&&text[i+1]==='"'){value+='"';i++;}else quoted=!quoted;}else if(c===','&&!quoted){row.push(value);value='';}else if(c==='\n'&&!quoted){row.push(value.replace(/\r$/,''));rows.push(row);row=[];value='';}else value+=c;}if(value||row.length){row.push(value);rows.push(row);}return rows;}
try{
  const context=await browser.newContext({viewport:{width:1440,height:1100},deviceScaleFactor:1});
  const page=await context.newPage(),errors=[],external=[],failed=[];
  page.on('pageerror',e=>errors.push(e.message));page.on('request',r=>{if(/^https?:/.test(r.url())&&new URL(r.url()).origin!==new URL(base).origin)external.push(r.url());});page.on('requestfailed',r=>{if(!r.failure()?.errorText?.includes('ERR_ABORTED'))failed.push({url:r.url(),error:r.failure()?.errorText});});
  const api=async suffix=>(await context.request.get(base+suffix)).json();
  const meta=await api('/api/meta?corpus=core'),labels=meta.layers.find(l=>l.scheme==='topics'&&l.level==='field').labels.slice(0,3),ids=labels.map(l=>l.id);
  const params=corpus=>new URLSearchParams({corpus,scenario:meta.eci_scenario_id,scheme:'topics',level:'field',disciplines:ids.join(','),countries:countries.join(','),metric:'publications',basis:'publications',start:'2000',end:'2025'});
  const body=()=>page.evaluate(async()=>{const r=await fetch('/api/series'+location.search),b=await r.json();if(!r.ok)throw new Error(b.error);return b;});
  const settle=async expected=>{
    await page.waitForFunction(e=>{const chart=window.echarts?.getInstanceByDom(document.querySelector('#chart'));if(!chart)return false;const opt=chart.getOption();if(opt.series?.length!==e.series.length)return false;const rank=['eci','pci'].includes(e.query.metric)&&document.querySelector('[data-model-view="rank"]').getAttribute('aria-pressed')==='true';for(let i=0;i<e.series.length;i++){for(let j=0;j<e.series[i].points.length;j++){const p=e.series[i].points[j],v=rank?p.rank:p.value==null?null:e.query.metric==='share'?p.value*100:p.value;if(opt.series[i].data[j]!==v)return false;}}return document.querySelectorAll('#comparisonBody tr').length===e.series.length;},expected,{timeout:30000});
  };
  const navigate=async p=>{await page.goto(base+'/?'+p);await page.waitForLoadState('networkidle');await page.waitForFunction(()=>document.querySelector('#workspace')?.hidden===false&&window.echarts?.getInstanceByDom(document.querySelector('#chart')));const b=await body();await settle(b);return b;};
  for(const corpus of ['core','all']){
    const p=params(corpus);let b=await navigate(p);
    assert.equal(b.series.length,27);assert.equal(new Set(b.series.map(s=>s.id)).size,27);assert.ok(b.series.every(s=>countries.includes(s.country)&&ids.includes(s.discipline)&&s.discipline_name));
    assert.equal(await page.locator('#selectedDisciplines button').count(),3);assert.match(await page.locator('#curveCount').textContent(),/9.*3.*27/);
    assert.equal(await page.locator('[aria-label="知识复杂度"] [data-metric]').count(),2);assert.equal(await page.locator('[aria-label="数量与引用"] [data-metric]').count(),3);assert.equal(await page.locator('[aria-label="比较优势"] [data-metric="rca"]').count(),1);
    const options=await page.evaluate(()=>window.echarts.getInstanceByDom(document.querySelector('#chart')).getOption());
    assert.equal(new Set(options.series.slice(0,3).map(s=>s.lineStyle.type)).size,3);assert.ok(options.series.slice(0,3).every(s=>s.lineStyle.color===options.series[0].lineStyle.color));
    check(corpus+'_27_country_subject_curves_groups_and_styles',{countries:9,disciplines:3,series:27});
    for(const metric of ['publications','citations','share','rca']){
      await page.locator(`[data-metric="${metric}"]`).click();b=await body();await settle(b);
      const selected=b.series.find(s=>s.country==='CN'&&s.discipline===ids[1]),single=new URLSearchParams({...Object.fromEntries(p),metric,discipline:ids[1],countries:'CN'});single.delete('disciplines');const one=await api('/api/series?'+single);
      assert.deepEqual(selected.points,one.series[0].points,metric+' values/margins changed with subject selection');
    }
    check(corpus+'_selection_preserves_count_share_rca_denominators',null);
    await page.locator('[data-metric="publications"]').click();b=await body();await settle(b);
    const csvResponse=await context.request.get(base+'/api/series.csv'+new URL(page.url()).search),rows=csv(await csvResponse.text());assert.equal(rows.length,27*26+1);assert.equal(rows[0][14],'series_id');for(const row of rows.slice(1)){const s=b.series.find(s=>s.id===row[14]);assert.ok(s);assert.equal(row[3],s.discipline);assert.equal(row[6],s.country);assert.equal(row[11],corpus);}
    check(corpus+'_csv_has_every_pair_and_corpus',{rows:rows.length-1});
    await page.evaluate(()=>window.echarts.getInstanceByDom(document.querySelector('#chart')).dispatchAction({type:'showTip',seriesIndex:0,dataIndex:25}));await page.waitForFunction(()=>document.querySelectorAll('[data-tooltip-series]').length===27);
    const tooltip=await page.locator('[data-tooltip-series]').evaluateAll(rs=>rs.map(r=>({id:r.dataset.tooltipSeries,value:Number(r.dataset.tooltipValue),flag:r.querySelector('img')?.getAttribute('src')})));for(let i=1;i<tooltip.length;i++)assert.ok(tooltip[i-1].value>=tooltip[i].value);assert.ok(tooltip.every(r=>r.flag===`/static/flags/${b.series.find(s=>s.id===r.id).country.toLowerCase()}.svg`));
    const scroll=await page.locator('.tooltip-list').evaluate(e=>({height:e.clientHeight,scroll:e.scrollHeight}));assert.ok(scroll.height<=361&&scroll.scroll>scroll.height);
    await page.screenshot({path:path.join(out,`${corpus}-desktop-27-tooltip.png`),fullPage:true,animations:'disabled'});await page.evaluate(()=>window.echarts.getInstanceByDom(document.querySelector('#chart')).dispatchAction({type:'hideTip'}));
    await page.locator(`[data-remove-discipline="${ids[1]}"]`).click();let reduced=await body();await settle(reduced);assert.equal(reduced.series.length,18);await page.reload();await page.waitForLoadState('networkidle');reduced=await body();await settle(reduced);assert.equal(reduced.series.length,18);
    await page.locator('#disciplineSummary').click();await page.locator('#disciplineSearch').fill(ids[1]);await page.locator(`[data-discipline-id="${ids[1]}"]:not(:disabled)`).first().click();await page.locator('#disciplineSummary').click();b=await body();await settle(b);assert.equal(b.series.length,27);
    check(corpus+'_multi_tree_chips_and_shareable_url',null);
    await page.locator('[data-metric="eci"]').click();const eci=await body();await settle(eci);assert.equal(eci.series.length,9);assert.ok(eci.series.every(s=>s.discipline===''&&s.id===s.country));assert.equal(await page.locator('#selectedDisciplines button:disabled').count(),3);
    const rank=eci.series.find(s=>s.country==='CN').points.at(-1).rank,onlyCN=new URLSearchParams(new URL(page.url()).search);onlyCN.set('countries','CN');const one=await api('/api/series?'+onlyCN);assert.equal(one.series[0].points.at(-1).rank,rank);
    check(corpus+'_eci_country_only_and_global_rank',{curves:9,cn_rank:rank});
    await page.locator('[data-metric="pci"]').click();const pci=await body();await settle(pci);assert.equal(pci.series.length,3);assert.ok(pci.series.every(s=>s.country==='GLOBAL'));assert.equal(await page.locator('#comparisonBody img.country-flag').count(),0);
    await page.evaluate(()=>window.echarts.getInstanceByDom(document.querySelector('#chart')).dispatchAction({type:'showTip',seriesIndex:0,dataIndex:25}));await page.waitForFunction(()=>document.querySelectorAll('[data-tooltip-series]').length===3);assert.equal(await page.locator('[data-tooltip-series] img.country-flag').count(),0);
    await page.screenshot({path:path.join(out,`${corpus}-desktop-pci-rank.png`),fullPage:true,animations:'disabled'});
    await page.locator('[data-model-view="score"]').click();await settle(pci);await page.locator('[data-model-view="rank"]').click();await settle(pci);
    check(corpus+'_pci_subject_only_rank_score_and_neutral_global',{curves:3});
    const before=new URL(page.url()).searchParams.get('disciplines'),other=corpus==='core'?'all':'core';await page.locator(`[data-corpus="${other}"]`).click();await page.waitForURL(u=>u.searchParams.get('corpus')===other);await page.waitForLoadState('networkidle');const switched=await body();await settle(switched);assert.equal(switched.query.corpus,other);assert.equal(new URL(page.url()).searchParams.get('disciplines'),before);assert.equal(switched.series.length,3);
    check(corpus+'_corpus_switch_preserves_multi_selection',null);
  }
  const children=meta.layers.find(l=>l.scheme==='topics'&&l.level==='subfield').labels.filter(l=>l.parent_ids?.length).sort((a,b)=>b.display_name.length-a.display_name.length).slice(0,3),mobile=params('all');mobile.set('level','subfield');mobile.set('disciplines',children.map(l=>l.id).join(','));await page.setViewportSize({width:390,height:844});let b=await navigate(mobile);assert.equal(b.series.length,27);
  await page.locator('#disciplineSummary').click();await page.locator('#disciplineSearch').fill(children[0].id);assert.equal(await page.locator(`[data-discipline-id="${children[0].id}"]:not(:disabled)`).count(),1);await page.screenshot({path:path.join(out,'mobile-long-subfield-tree.png'),fullPage:true,animations:'disabled'});await page.locator('#disciplineSummary').click();
  await page.locator('#chart').scrollIntoViewIfNeeded();await page.evaluate(()=>window.echarts.getInstanceByDom(document.querySelector('#chart')).dispatchAction({type:'showTip',seriesIndex:0,dataIndex:25}));await page.waitForFunction(()=>document.querySelectorAll('[data-tooltip-series]').length===27);await page.screenshot({path:path.join(out,'mobile-27-long-subfield-tooltip.png'),fullPage:true,animations:'disabled'});
  const layout=await page.evaluate(()=>({width:innerWidth,scroll:document.documentElement.scrollWidth,body:getComputedStyle(document.body).fontSize,control:getComputedStyle(document.querySelector('#countrySearch')).fontSize,broken:[...document.querySelectorAll('img.country-flag')].filter(i=>!i.complete||!i.naturalWidth).length,chart:window.echarts.getInstanceByDom(document.querySelector('#chart')).getOption().xAxis[0].axisLabel.fontSize}));assert.ok(layout.scroll<=layout.width,JSON.stringify(layout));assert.equal(layout.broken,0);assert.equal(layout.body,'16px');assert.ok(layout.chart>=12);
  check('mobile_long_subjects_27_curves_flags_fonts_no_overflow',layout);
  const reject=new URLSearchParams(mobile);reject.set('disciplines',meta.layers.find(l=>l.scheme==='topics'&&l.level==='subfield').labels.slice(0,6).map(l=>l.id).join(','));const rejected=await context.request.get(base+'/api/series?'+reject);assert.equal(rejected.status(),400);assert.match((await rejected.json()).error,/54.*48/);check('over_limit_rejected_without_truncation',{requested:54});
  assert.deepEqual(errors,[]);assert.deepEqual(external,[]);assert.deepEqual(failed,[]);check('no_errors_external_requests_or_broken_assets',null);
  report.status='passed';
}catch(error){report.status='failed';report.failure=error.stack||String(error);throw error;}finally{report.completed_at=new Date().toISOString();await fs.writeFile(path.join(out,'feature-qa.json'),JSON.stringify(report,null,2));await browser.close();console.log(JSON.stringify({status:report.status,checks:report.checks.length,output:out,failure:report.failure}));}
