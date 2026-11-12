// Real-service visual acceptance for the normal-zoom reading scale.
import {createRequire} from 'node:module';
import fs from 'node:fs/promises';
import path from 'node:path';
import assert from 'node:assert/strict';
const require=createRequire(import.meta.url);
const {chromium}=require('../bin/browser-qa/node_modules/playwright');
const base=process.env.QA_URL||'http://192.168.1.229:8901';
const out=path.resolve(process.env.QA_OUTPUT||'research/ui-typography-v11/visual-candidate');
await fs.mkdir(out,{recursive:true});
const report={url:base,asset_override:false,started_at:new Date().toISOString(),checks:[]};
const browser=await chromium.launch({channel:'chrome',headless:true});
try{
 for(const [viewport,width,height] of [['desktop',1440,1100],['mobile',390,844]]){
  const page=await browser.newPage({viewport:{width,height}}),errors=[];
  page.on('pageerror',e=>errors.push(e.message));
  const open=async params=>{const url=new URL(base);url.search=new URLSearchParams({corpus:'core',...params});await page.goto(url.toString(),{waitUntil:'networkidle'});await page.waitForFunction(()=>document.querySelector('#chart canvas')&&document.querySelector('#chartDefinition').textContent&&document.querySelector('#chartError').hidden);};
  await open({});
  const inspect=()=>page.evaluate(()=>{
   const chart=echarts.getInstanceByDom(document.getElementById('chart')),option=chart.getOption();
   const selectors=['body','.control-block legend','.control-block label','.switch button','#yearStart','#countrySearch','.country-option','.selected-countries button','.tree-picker>summary','.hint','.metric-tabs button','.metric-description','.chart-footer p','table','th','td','.method-panel>p'];
   const fonts=Object.fromEntries(selectors.map(s=>[s,getComputedStyle(document.querySelector(s)).fontSize]));
   return {fonts,chart:{width:chart.getWidth(),height:chart.getHeight(),legend:option.legend[0].textStyle.fontSize,x_axis:option.xAxis[0].axisLabel.fontSize,y_axis:option.yAxis[0].axisLabel.fontSize,tooltip:option.tooltip[0].textStyle.fontSize,names:option.series.map(s=>s.name)},page_width:innerWidth,content_width:document.documentElement.scrollWidth};
  });
  const publication=await inspect();
  assert.equal(publication.content_width,width,'page has horizontal overflow');
  assert.ok(parseFloat(publication.fonts.body)>=16);
  for(const key of ['#yearStart','#countrySearch','.country-option','.tree-picker>summary'])assert.ok(parseFloat(publication.fonts[key])>=15,key);
  assert.ok(publication.chart.x_axis>=12&&publication.chart.y_axis>=12&&publication.chart.legend>=13&&publication.chart.tooltip>=14);
  assert.equal(publication.chart.names.length,9);
  await page.locator('.chart-panel').screenshot({path:path.join(out,`${viewport}-publications.png`),animations:'disabled'});
  await open({metric:'eci'});
  await page.waitForFunction(()=>echarts.getInstanceByDom(document.querySelector('#chart')).getOption().yAxis[0].inverse===true);
  const bump=await inspect();assert.ok(bump.chart.x_axis>=12&&bump.chart.legend>=13&&bump.chart.tooltip>=14);
  await page.locator('.chart-panel').screenshot({path:path.join(out,`${viewport}-bump.png`),animations:'disabled'});
  await page.locator('#chart').scrollIntoViewIfNeeded();
  const pixel=await page.evaluate(()=>{const el=document.querySelector('#chart'),chart=echarts.getInstanceByDom(el),option=chart.getOption(),i=option.series[0].data.findLastIndex(v=>v!==null),point=chart.convertToPixel({seriesIndex:0},[i,option.series[0].data[i]]),r=el.getBoundingClientRect();return {x:r.x+point[0],y:r.y+point[1]};});
  await page.mouse.move(pixel.x,pixel.y);await page.waitForFunction(()=>document.querySelectorAll('[data-tooltip-series]').length===9);
  const tooltip=await page.locator('[data-tooltip-series]').first().evaluate(row=>{const tip=row.parentElement,r=tip.getBoundingClientRect(),c=document.querySelector('#chart').getBoundingClientRect();return {font:getComputedStyle(row).fontSize,width:r.width,left:r.left,right:r.right,chart_left:c.left,chart_right:c.right,rank_rows:[...tip.querySelectorAll('[data-tooltip-rank]')].map(r=>({id:r.dataset.tooltipSeries,rank:Number(r.dataset.tooltipRank)}))};});
  assert.ok(parseFloat(tooltip.font)>=14);
  assert.ok(tooltip.left>=tooltip.chart_left-1&&tooltip.right<=tooltip.chart_right+1,'tooltip overflows chart');
  assert.ok(tooltip.rank_rows.every((r,i,a)=>!i||r.rank>=a[i-1].rank));
  await page.locator('.chart-panel').screenshot({path:path.join(out,`${viewport}-tooltip.png`),animations:'disabled'});
  await page.mouse.move(0,0);await page.evaluate(()=>echarts.getInstanceByDom(document.querySelector('#chart')).dispatchAction({type:'hideTip'}));
  await page.locator('[data-model-view="score"]').click();
  assert.equal(await page.evaluate(()=>Boolean(echarts.getInstanceByDom(document.querySelector('#chart')).getOption().yAxis[0].inverse)),false);
  await page.locator('.chart-panel').screenshot({path:path.join(out,`${viewport}-score.png`),animations:'disabled'});
  const meta=await page.evaluate(async()=>(await fetch('/api/meta?corpus=core')).json());
  const child=meta.layers.find(l=>l.scheme==='topics'&&l.level==='subfield').labels.filter(l=>l.parent_ids?.length).sort((a,b)=>b.display_name.length-a.display_name.length)[0];
  await open({scheme:'topics',level:'subfield',discipline:child.id});
  await page.locator('#disciplineSummary').click();await page.locator('#disciplineSearch').fill(child.display_name);
  const node=page.locator(`[data-discipline-id="${child.id}"]:not(:disabled)`).first();await node.waitFor({state:'visible'});
  const tree=await node.evaluate(el=>({font:getComputedStyle(el).fontSize,client:el.clientWidth,scroll:el.scrollWidth,text:el.textContent}));
  assert.ok(parseFloat(tree.font)>=15&&tree.scroll<=tree.client+1);
  await page.locator('.tree-popover').screenshot({path:path.join(out,`${viewport}-tree.png`),animations:'disabled'});
  await page.locator('#disciplinePicker').evaluate(el=>el.open=false);
  await page.locator('#countrySearch').fill('United Kingdom');assert.equal(await page.locator('#countryList input[value="GB"]').count(),1);
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth),width);
  await page.screenshot({path:path.join(out,`${viewport}-page.png`),fullPage:true,animations:'disabled'});
  assert.deepEqual(errors,[]);
  report.checks.push({viewport,width,status:'passed',publication,bump,tooltip,tree,errors});
  await page.close();
 }
 report.status='passed';
}catch(error){report.status='failed';report.error=error.stack;throw error;}
finally{report.completed_at=new Date().toISOString();await fs.writeFile(path.join(out,'typography-qa.json'),JSON.stringify(report,null,2));await browser.close();console.log(JSON.stringify({status:report.status,output:out,error:report.error}));}
