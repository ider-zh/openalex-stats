// Full-dataset protocol parity against the immutable Go reference service.
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import {createHash} from 'node:crypto';
const edge=process.env.PAGES_URL||'http://127.0.0.1:8788',native=process.env.GO_URL||'http://192.168.1.229:8901';
const output=path.resolve(process.env.PARITY_OUTPUT||'research/cloudflare-delivery/pages-parity.json');
const comparison_policy={metadata:'Country and discipline metadata arrays compared as ID-keyed semantic sets: duplicate display names can have different Go map iteration order.',series:'Requested series order, point values, statuses, global ranks, definitions and query fields compared strictly.',csv:'Text cells and row order strict; numeric cells compared as Float64 values.'};
const report={edge,native,comparison_policy,started_at_utc:new Date().toISOString(),cases:0,point_values:0,csv_cases:0,download_cases:0,invalid_cases:0,status:'running'};
async function get(base,url){const r=await fetch(base+url);return {status:r.status,body:await r.json()};}
function canonicalMeta(value){const copy=structuredClone(value);copy.body.countries.sort((a,b)=>a.id.localeCompare(b.id));for(const layer of copy.body.layers)layer.labels.sort((a,b)=>a.id.localeCompare(b.id));return copy;}
function csv(text){const rows=[];let row=[],cell='',quoted=false;for(let i=0;i<text.length;i++){const c=text[i];if(c==='"'){if(quoted&&text[i+1]==='"'){cell+='"';i++;}else quoted=!quoted;}else if(c===','&&!quoted){row.push(cell);cell='';}else if(c==='\n'&&!quoted){row.push(cell);rows.push(row);row=[];cell='';}else if(c!=='\r'||quoted)cell+=c;}return rows;}
async function compare(params,asCSV=false){
  const url=`/api/series${asCSV?'.csv':''}?${new URLSearchParams(params)}`;
  if(!asCSV){const [a,b]=await Promise.all([get(edge,url),get(native,url)]);assert.equal(a.status,b.status,url);try{assert.deepEqual(a.body,b.body);}catch(error){throw new Error(`${url}: ${error.message}`);}report.cases++;report.point_values+=a.body.series?.reduce((n,s)=>n+s.points.length,0)||0;return;}
  const [a,b]=await Promise.all([fetch(edge+url),fetch(native+url)]);assert.equal(a.status,b.status,url);const rowsA=csv(await a.text()),rowsB=csv(await b.text());assert.deepEqual(rowsA[0],rowsB[0]);assert.equal(rowsA.length,rowsB.length);const numeric=new Set(['year','value','rank','rank_universe','country_iterations','discipline_iterations']);for(let i=1;i<rowsA.length;i++)for(let j=0;j<rowsA[0].length;j++){if(numeric.has(rowsA[0][j])&&rowsA[i][j]!==''&&rowsB[i][j]!=='')assert.equal(Number(rowsA[i][j]),Number(rowsB[i][j]),`CSV numeric ${i}/${j}`);else assert.equal(rowsA[i][j],rowsB[i][j],`CSV cell ${i}/${j}`);}report.csv_cases++;
}
try{
  const [catalogA,catalogB]=await Promise.all([get(edge,'/api/catalog'),get(native,'/api/catalog')]);assert.deepEqual(catalogA,catalogB);
  const jobs=[];
  for(const corpus of ['core','all']){
    const [a,b]=await Promise.all([get(edge,`/api/meta?corpus=${corpus}`),get(native,`/api/meta?corpus=${corpus}`)]);assert.deepEqual(canonicalMeta(a),canonicalMeta(b));const meta=a.body;
    for(const layer of meta.layers){const ids=[layer.labels[0].id,layer.labels[Math.floor(layer.labels.length/2)].id];for(const scenario of meta.scenarios)for(const method of meta.methods)for(const metric of ['publications','citations','share','rca','eci','pci'])for(const basis of (['publications','citations'].includes(metric)?['publications']:['publications','citations']))jobs.push({corpus,scheme:layer.scheme,level:layer.level,scenario:scenario.id,method:method.id,metric,basis,disciplines:ids.join(','),countries:'CN,US,GB',start:'2000',end:'2026'});}
    const layer=meta.layers[0],ids=layer.labels.slice(0,3).map(x=>x.id);
    for(const method of meta.methods){for(const metric of ['publications','citations','share','rca','eci','pci'])await compare({corpus,scheme:layer.scheme,level:layer.level,discipline:ids[0],countries:'CN,US,GB',method:method.id,metric,start:'2000',end:'2026'},true);}
    for(const [method,file] of [['spectral-v1','run.json'],['java-reflections-v1','run.json'],['java-reflections-v1','method-audit.json'],['java-reflections-v1','method-sensitivity.json'],['java-reflections-v1','country_eci.parquet'],['spectral-v1','country_eci.parquet']]){
      const url=`/download?${new URLSearchParams({corpus,method,file})}`;const [aa,bb]=await Promise.all([fetch(edge+url,{redirect:'manual'}),fetch(native+url,{redirect:'manual'})]);assert.equal(aa.status,bb.status,url);const [ab,bbuf]=await Promise.all([aa.arrayBuffer(),bb.arrayBuffer()]);assert.equal(createHash('sha256').update(new Uint8Array(ab)).digest('hex'),createHash('sha256').update(new Uint8Array(bbuf)).digest('hex'),url);assert.equal(aa.headers.get('Content-Disposition'),bb.headers.get('Content-Disposition'),url);report.download_cases++;
    }
    await compare({corpus,scheme:layer.scheme,level:layer.level,disciplines:ids.join(','),countries:'CN,US,GB,DE,FR,JP,IN,CA,KR',metric:'publications',start:'2000',end:'2025'});
  }
  let cursor=0;await Promise.all(Array.from({length:6},async()=>{while(cursor<jobs.length){const i=cursor++;await compare(jobs[i]);}}));
  const invalid=['/api/meta?corpus=bad','/api/meta?corpus=core&method=spectral-v1','/api/catalog?corpus=core','/api/series?scheme=topics&level=field&discipline=bad&countries=CN','/api/series?scheme=topics&level=field&metric=eci&countries=CN&method=bad','/api/series?scheme=topics&level=field&metric=eci&countries=BAD','/api/series?scheme=topics&level=field&metric=eci&countries=CN&start=2000.5','/api/series?scheme=topics&level=field&metric=eci&countries=CN&start=1999','/api/series?scheme=topics&level=field&metric=eci&countries=CN&metric=eci','/download?file=../secret','/download?file=run.json&method=bad','/download?file=run.json&file=eci.json'];
  for(const url of invalid){const [a,b]=await Promise.all([get(edge,url),get(native,url)]);assert.equal(a.status,b.status,url);assert.deepEqual(a.body,b.body,url);report.invalid_cases++;}
  report.status='passed';
}catch(error){report.status='failed';report.failure=String(error.stack||error).slice(0,7000);console.error(report.failure);process.exitCode=1;}
finally{report.completed_at_utc=new Date().toISOString();await fs.mkdir(path.dirname(output),{recursive:true});await fs.writeFile(output,JSON.stringify(report,null,2)+'\n');console.log(JSON.stringify(report,null,2));}
