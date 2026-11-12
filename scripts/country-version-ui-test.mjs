// Live UI acceptance: every visible/exported result stays bound to one country version.
import {createRequire} from 'node:module';
import fs from 'node:fs/promises';
import path from 'node:path';
import assert from 'node:assert/strict';
const require=createRequire(import.meta.url),{chromium}=require('../bin/browser-qa/node_modules/playwright');
const base=process.env.QA_URL||'http://127.0.0.1:8898',out=path.resolve(process.env.QA_OUTPUT||'research/country-enriched-release-v1/ui');
await fs.mkdir(out,{recursive:true});
const report={status:'running',url:base,checks:[],started_at:new Date().toISOString()};
const record=(name,detail)=>report.checks.push({name,passed:true,detail});
const browser=await chromium.launch({channel:'chrome',headless:true});
const java='java-reflections-v1',spectral='spectral-v1';
let page;
function csv(text){const rows=[];let row=[],value='',quoted=false;for(let i=0;i<text.length;i++){const c=text[i];if(c==='"'){if(quoted&&text[i+1]==='"'){value+='"';i++;}else quoted=!quoted;}else if(c===','&&!quoted){row.push(value);value='';}else if(c==='\n'&&!quoted){row.push(value.replace(/\r$/,''));rows.push(row);row=[];value='';}else value+=c;}if(value||row.length){row.push(value);rows.push(row);}return rows;}
try{
  const context=await browser.newContext({viewport:{width:1440,height:1100},deviceScaleFactor:1});
  await context.addInitScript(()=>localStorage.setItem('openalex-atlas:onboarding:v1',JSON.stringify({version:1,dismissedAt:Date.now()})));
  page=await context.newPage();const errors=[],external=[];
  page.on('pageerror',e=>errors.push(e.message));
  page.on('request',r=>{if(/^https?:/.test(r.url())&&new URL(r.url()).origin!==new URL(base).origin)external.push(r.url());});
  const get=async suffix=>{const r=await context.request.get(base+suffix);assert.ok(r.ok(),`${r.status()} ${suffix}`);return r.json();};
  const body=()=>page.evaluate(async()=>{const r=await fetch('/api/series'+location.search),b=await r.json();if(!r.ok)throw new Error(b.error);return b;});
  const settle=async expected=>page.waitForFunction(e=>{const chart=window.echarts?.getInstanceByDom(document.querySelector('#chart'));if(!chart)return false;const opt=chart.getOption();if(opt.series?.length!==e.series.length)return false;const rank=['eci','pci'].includes(e.query.metric)&&document.querySelector('[data-model-view="rank"]').getAttribute('aria-pressed')==='true';return opt.series.every((s,i)=>s.data.length===e.series[i].points.length&&s.data.every((v,j)=>{const p=e.series[i].points[j];return v===(rank?p.rank:p.value==null?null:e.query.metric==='share'?p.value*100:p.value);}));},expected,{timeout:30000});
  const ready=async()=>{await page.waitForLoadState('networkidle');await page.waitForFunction(()=>document.documentElement.dataset.openalexReady==='true'||document.querySelector('#notReady')?.hidden===false||document.querySelector('#chartError')?.hidden===false);assert.equal(await page.locator('#notReady').isVisible(),false,await page.locator('#notReadyReason').textContent());assert.equal(await page.locator('#chartError').isVisible(),false,await page.locator('#chartError').textContent());const b=await body();await settle(b);assert.equal(await page.locator('#chartError').isVisible(),false);return b;};
  const navigate=async p=>{await page.goto(base+'/?'+p,{waitUntil:'networkidle'});return ready();};
  const boundLinks=async(version,corpus)=>{for(const id of ['downloadSeries','methodSensitivity']){const u=new URL(await page.locator('#'+id).getAttribute('href'),base);assert.equal(u.searchParams.get('country_version'),version);assert.equal(u.searchParams.get('corpus'),corpus);}for(const href of await page.locator('#fileDownloads a').evaluateAll(nodes=>nodes.map(n=>n.getAttribute('href')))){const u=new URL(href,base);assert.equal(u.searchParams.get('country_version'),version);assert.equal(u.searchParams.get('corpus'),corpus);}};
  const catalog=await get('/api/catalog');
  assert.equal(catalog.default_country_version,'enriched');assert.deepEqual(catalog.country_versions.map(v=>v.id).sort(),['enriched','original']);
  await page.goto(base,{waitUntil:'networkidle'});let b=await ready();assert.equal(b.query.country_version,'enriched');assert.equal(b.query.method,java);assert.equal(new URL(page.url()).searchParams.get('country_version'),'enriched');assert.equal(await page.locator('[data-country-version="enriched"]').getAttribute('aria-pressed'),'true');
  record('default_is_enriched_and_xiaoni',{country_version:b.query.country_version,method:b.query.method});
  for(const corpus of ['core','all']){
    const originalMeta=await get(`/api/meta?corpus=${corpus}&country_version=original`),enrichedMeta=await get(`/api/meta?corpus=${corpus}&country_version=enriched`);
    assert.equal(originalMeta.country_version,'original');assert.equal(enrichedMeta.country_version,'enriched');assert.equal(originalMeta.selected_works,enrichedMeta.selected_works);assert.deepEqual(originalMeta.graph,enrichedMeta.graph);assert.ok(enrichedMeta.quality.missing_country_publications<originalMeta.quality.missing_country_publications);
    const subjects=originalMeta.layers.find(l=>l.scheme==='topics'&&l.level==='field').labels.slice(0,2).map(l=>l.id);
    const p=new URLSearchParams({country_version:'original',corpus,method:java,scenario:originalMeta.eci_scenario_id,scheme:'topics',level:'field',disciplines:subjects.join(','),countries:'CN,US,GB',metric:'publications',basis:'publications',start:'2000',end:'2025'});
    b=await navigate(p);assert.equal(b.query.country_version,'original');assert.equal(b.series.length,6);await boundLinks('original',corpus);const before=new URLSearchParams(new URL(page.url()).search);
    await page.locator('[data-country-version="enriched"]').click();b=await ready();assert.equal(b.query.country_version,'enriched');const after=new URLSearchParams(new URL(page.url()).search);before.set('country_version','enriched');assert.deepEqual([...after].sort(),[...before].sort());await boundLinks('enriched',corpus);assert.match(await page.locator('#runBadge').textContent(),/国家补全版/);assert.match(await page.locator('#chartDefinition').textContent(),/^国家补全版/);
    await page.reload({waitUntil:'networkidle'});b=await ready();assert.equal(b.query.country_version,'enriched');
    record(corpus+'_switch_preserves_all_filters_and_changes_coverage',{original_missing:originalMeta.quality.missing_country_publications,enriched_missing:enrichedMeta.quality.missing_country_publications,curves:b.series.length});
    for(const version of ['original','enriched'])for(const method of [java,spectral])for(const metric of ['eci','pci']){
      const q=new URLSearchParams(p);q.set('country_version',version);q.set('method',method);q.set('metric',metric);b=await navigate(q);assert.equal(b.query.country_version,version);assert.equal(b.method.id,method);assert.equal(b.method_applies,true);assert.equal(b.series.length,metric==='eci'?3:2);await boundLinks(version,corpus);
      const rows=csv(await (await context.request.get(base+await page.locator('#downloadSeries').getAttribute('href'))).text()),vi=rows[0].indexOf('country_version'),mi=rows[0].indexOf('method');assert.ok(vi>=0);assert.ok(mi>=0);assert.ok(rows.slice(1).filter(r=>r.length>1).every(r=>r[vi]===version&&r[mi]===method));
      for(const file of ['eci.json','country_eci.parquet','discipline_pci.parquet']){const u=new URL(await page.locator(`#fileDownloads a[href*="file=${file}"]`).getAttribute('href'),base);assert.equal(u.searchParams.get('method'),method);}
    }
    record(corpus+'_both_versions_both_methods_eci_pci_csv_and_artifact_links',{combinations:8});
    const other=corpus==='core'?'all':'core';await page.locator(`[data-corpus="${other}"]`).click();b=await ready();assert.equal(b.query.country_version,'enriched');assert.equal(b.query.method,spectral);assert.equal(b.query.corpus,other);assert.equal(b.query.metric,'pci');assert.equal(b.series.length,2);
    record(corpus+'_corpus_switch_preserves_country_version_and_model',null);
  }
  await page.setViewportSize({width:390,height:844});await page.reload({waitUntil:'networkidle'});await ready();await page.locator('#countryVersionHelp').click();assert.equal(await page.locator('#provenance').getAttribute('open')!==null,true);assert.match(await page.locator('#countryVersionMethod').textContent(),/255 \/ 256/);assert.match(await page.locator('#countryVersionMethod').textContent(),/不是全部国家标签的整体准确率/);assert.match(await page.locator('#countryVersionMethod').textContent(),/49\.3127%.*50\.3009%/s);
  const layout=await page.evaluate(()=>({width:innerWidth,scroll:document.documentElement.scrollWidth,control:getComputedStyle(document.querySelector('[data-country-version]')).fontSize}));assert.ok(layout.scroll<=layout.width,JSON.stringify(layout));assert.ok(Number.parseInt(layout.control)>=14);await page.screenshot({path:path.join(out,'mobile-enrichment-method.png'),fullPage:false,animations:'disabled'});
  await page.locator('[data-country-version="original"]').click();b=await ready();assert.equal(b.query.country_version,'original');await page.reload({waitUntil:'networkidle'});b=await ready();assert.equal(b.query.country_version,'original');await page.locator('#countryVersionButtons').scrollIntoViewIfNeeded();await page.screenshot({path:path.join(out,'mobile-original-switch.png'),fullPage:false,animations:'disabled'});
  record('mobile_method_disclosure_and_explicit_original_reload',layout);
  await page.setViewportSize({width:1440,height:1100});await page.locator('[data-country-version="enriched"]').click();await ready();await page.locator('#countryVersionButtons').scrollIntoViewIfNeeded();await page.screenshot({path:path.join(out,'desktop-enriched-switch.png'),fullPage:false,animations:'disabled'});
  // A deliberately mismatched API response must never be drawn under the wrong label.
  await page.route('**/api/series?**',async route=>{const r=await route.fetch(),data=await r.json();data.query.country_version=data.query.country_version==='enriched'?'original':'enriched';await route.fulfill({response:r,json:data});});
  await page.locator('[data-metric="publications"]').click();await page.waitForFunction(()=>!document.querySelector('#chartError').hidden&&document.querySelector('#chartError').textContent.includes('国家版本不匹配'));await page.unroute('**/api/series?**');
  record('mismatched_response_is_rejected_before_chart_draw',null);
  assert.deepEqual(errors,[]);assert.deepEqual(external,[]);record('no_page_errors_or_external_requests',null);report.status='passed';
}catch(error){report.status='failed';report.failure=error.stack||String(error);if(page){try{report.dom=await page.evaluate(()=>({url:location.href,not_ready:document.querySelector('#notReadyReason')?.textContent,chart_error:document.querySelector('#chartError')?.textContent}));await page.screenshot({path:path.join(out,'failure.png'),fullPage:false});}catch{}}throw error;}finally{report.completed_at=new Date().toISOString();await fs.writeFile(path.join(out,'country-version-ui-qa.json'),JSON.stringify(report,null,2));await browser.close();console.log(JSON.stringify({status:report.status,checks:report.checks.length,output:out,failure:report.failure}));}
