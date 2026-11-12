// Actual four-dataset Go/Pages parity. Does not modify or recompute science data.
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import {createHash} from 'node:crypto';

const edge=(process.env.PAGES_URL||'http://127.0.0.1:8798').replace(/\/$/,''),native=(process.env.GO_URL||'http://192.168.1.229:8898').replace(/\/$/,'');
const output=path.resolve(process.env.PARITY_OUTPUT||'research/country-enriched-release-v1/parity/pages-parity.json');
const staticRoot=path.resolve(process.env.STATIC_ROOT||'cloudflare/data');
const concurrency=Number(process.env.PARITY_WORKERS||6);
assert.ok(Number.isInteger(concurrency)&&concurrency>=1&&concurrency<=12);
const report={status:'running',edge,native,started_at_utc:new Date().toISOString(),cases:0,point_values:0,csv_cases:0,metadata_cases:0,small_downloads:0,large_download_heads:0,release_head_requested:process.env.CHECK_RELEASE_HEAD==='1',release_head_checks:0,invalid_cases:0,head_cases:0,default_cases:0,version_difference_cases:0,datasets:[],comparison_policy:{metadata:'ID-keyed arrays are semantic sets; labels are not keyed by display names.',series:'All query fields including country_version, all values/statuses/global ranks/universes, methods/source_run_id and series order are exact.',csv:'Header/order/text exact; numeric cells parsed as Float64.',downloads:'Small bodies byte-SHA match. Base completion is verified from the immutable static artifact (not public download allowlist) and bound to actual Go run.json bytes; method completion is live-downloaded. Large Go HEAD length and completion artifact SHA must match selected static control and Pages redirect; remote Release HEAD optional via CHECK_RELEASE_HEAD=1.'}};
const hash=bytes=>createHash('sha256').update(bytes).digest('hex');
async function request(base,url,options={}){return fetch(base+url,{signal:AbortSignal.timeout(60000),redirect:'manual',...options});}
async function json(base,url){const r=await request(base,url);return {status:r.status,body:await r.json(),version:r.headers.get('X-OpenAlex-Country-Version'),corpus:r.headers.get('X-OpenAlex-Corpus')};}
function canonicalMeta(meta){const copy=structuredClone(meta);for(const field of ['countries','methods','scenarios','country_versions'])if(copy[field])copy[field].sort((a,b)=>a.id.localeCompare(b.id));if(copy.downloads)copy.downloads.sort();if(copy.layers){copy.layers.sort((a,b)=>`${a.scheme}/${a.level}`.localeCompare(`${b.scheme}/${b.level}`));for(const layer of copy.layers)layer.labels.sort((a,b)=>a.id.localeCompare(b.id));}return copy;}
function csv(text){const rows=[];let row=[],cell='',quoted=false;for(let i=0;i<text.length;i++){const c=text[i];if(c==='"'){if(quoted&&text[i+1]==='"'){cell+='"';i++;}else quoted=!quoted;}else if(c===','&&!quoted){row.push(cell);cell='';}else if(c==='\n'&&!quoted){row.push(cell);rows.push(row);row=[];cell='';}else if(c!=='\r'||quoted)cell+=c;}assert.equal(quoted,false,'CSV unclosed quote');return rows;}
const scienceByCase=new Map(),differenceWitness=[];
async function compare(params,asCSV=false){
  const url=`/api/series${asCSV?'.csv':''}?${new URLSearchParams(params)}`;
  if(!asCSV){
    const [a,b]=await Promise.all([json(edge,url),json(native,url)]);assert.equal(a.status,200,url);assert.deepEqual(a,b,url);assert.equal(a.body.query.country_version,params.country_version);assert.equal(a.body.query.corpus,params.corpus);
    const control=site.country_versions[params.country_version][params.corpus];assert.equal(a.body.method.source_run_id,control.run_id,`wrong source_run_id ${url}`);
    report.cases++;report.point_values+=a.body.series.reduce((n,s)=>n+s.points.length,0);
    const key=new URLSearchParams(Object.fromEntries(Object.entries(params).filter(([k])=>k!=='country_version'))).toString(),science=JSON.stringify(a.body.series);
    const previous=scienceByCase.get(key);
    if(previous&&previous.version!==params.country_version&&previous.science!==science){report.version_difference_cases++;if(differenceWitness.length<8)differenceWitness.push({query:key,versions:[previous.version,params.country_version],first_fingerprint:hash(previous.science),second_fingerprint:hash(science)});}
    if(!previous)scienceByCase.set(key,{version:params.country_version,science});
    return a.body;
  }
  const [a,b]=await Promise.all([request(edge,url),request(native,url)]);assert.equal(a.status,200,url);assert.equal(b.status,200,url);assert.equal(a.headers.get('X-OpenAlex-Country-Version'),params.country_version);assert.equal(b.headers.get('X-OpenAlex-Country-Version'),params.country_version);
  const rowsA=csv(await a.text()),rowsB=csv(await b.text());assert.deepEqual(rowsA[0],rowsB[0],url);assert.equal(rowsA[0].at(-1),'country_version');assert.equal(rowsA.length,rowsB.length,url);
  const numeric=new Set(['year','value','rank','rank_universe','country_iterations','discipline_iterations']);
  for(let i=1;i<rowsA.length;i++){assert.equal(rowsA[i].at(-1),params.country_version);for(let j=0;j<rowsA[0].length;j++)if(numeric.has(rowsA[0][j])&&rowsA[i][j]!==''&&rowsB[i][j]!=='')assert.equal(Number(rowsA[i][j]),Number(rowsB[i][j]),`CSV ${url} numeric ${i}/${j}`);else assert.equal(rowsA[i][j],rowsB[i][j],`CSV ${url} text ${i}/${j}`);}
  report.csv_cases++;
}
async function smallDownload(params,expected){
  const url='/download?'+new URLSearchParams(params),[a,b]=await Promise.all([request(edge,url),request(native,url)]);assert.equal(a.status,200,url);assert.equal(b.status,200,url);
  const [aa,bb]=await Promise.all([a.arrayBuffer(),b.arrayBuffer()]);assert.equal(aa.byteLength,expected.bytes,url);assert.equal(bb.byteLength,expected.bytes,url);assert.equal(hash(new Uint8Array(aa)),expected.sha256,url);assert.equal(hash(new Uint8Array(bb)),expected.sha256,url);assert.equal(a.headers.get('X-Artifact-SHA256'),expected.sha256,url);assert.equal(a.headers.get('Content-Disposition'),b.headers.get('Content-Disposition'),url);for(const r of [a,b])assert.equal(r.headers.get('X-OpenAlex-Country-Version'),params.country_version,url);report.small_downloads++;return Buffer.from(bb);
}
let site;
try{
  site=JSON.parse(await fs.readFile(path.join(staticRoot,'data/site.json'),'utf8'));assert.ok(site.country_versions?.original&&site.country_versions?.enriched,'Need combined real country-version export');
  const catalogs=await Promise.all([json(edge,'/api/catalog'),json(native,'/api/catalog')]);assert.equal(catalogs[0].status,200);assert.deepEqual(canonicalMeta(catalogs[0].body),canonicalMeta(catalogs[1].body));assert.equal(catalogs[0].body.default_country_version,'enriched');
  const jobs=[],csvJobs=[],allCountries=[];
  for(const version of ['original','enriched'])for(const corpus of ['core','all']){
    const params={country_version:version,corpus},url='/api/meta?'+new URLSearchParams(params),[a,b]=await Promise.all([json(edge,url),json(native,url)]);assert.equal(a.status,200,url);assert.equal(b.status,200,url);assert.deepEqual(canonicalMeta(a.body),canonicalMeta(b.body),url);assert.equal(a.version,version);assert.equal(b.version,version);assert.equal(a.body.country_version,version);assert.equal(a.body.country_version_info.id,version);assert.equal(a.body.max_eci_countries,a.body.countries.length);report.metadata_cases++;
    const meta=a.body,control=site.country_versions[version][corpus];report.datasets.push({country_version:version,corpus,run_id:control.run_id,completion_sha256:control.completion_sha256,selected_works:meta.selected_works,countries:meta.countries.length,layers:meta.layers.length});
    for(const layer of meta.layers){
      const sorted=[...layer.labels].sort((x,y)=>x.id.localeCompare(y.id)),ids=[sorted[0].id,sorted[Math.floor(sorted.length/2)].id];
      for(const scenario of meta.scenarios)for(const method of meta.methods)for(const metric of ['publications','citations','share','rca','eci','pci'])for(const basis of (['publications','citations'].includes(metric)?['publications']:['publications','citations']))jobs.push({...params,scheme:layer.scheme,level:layer.level,scenario:scenario.id,method:method.id,metric,basis,disciplines:ids.join(','),countries:'CN,US,GB',start:'2000',end:'2026'});
      for(const method of meta.methods)for(const basis of ['publications','citations'])allCountries.push({...params,scheme:layer.scheme,level:layer.level,method:method.id,metric:'eci',basis,countries:meta.countries.map(c=>c.id).join(','),start:'2000',end:'2026'});
    }
    const layer=meta.layers[0],id=[...layer.labels].sort((x,y)=>x.id.localeCompare(y.id))[0].id;
    for(const method of meta.methods){for(const metric of ['publications','citations','share','rca','eci','pci'])csvJobs.push({...params,scheme:layer.scheme,level:layer.level,discipline:id,countries:'CN,US,GB',method:method.id,metric,start:'2000',end:'2026'});csvJobs.push({...params,scheme:layer.scheme,level:layer.level,countries:meta.countries.map(c=>c.id).join(','),method:method.id,metric:'eci',start:'2000',end:'2026'});}
    const baseArtifact=control.downloads['completion.json'],baseBytes=await fs.readFile(path.join(staticRoot,baseArtifact.path.replace(/^\//,'')));
    assert.equal(hash(baseBytes),baseArtifact.sha256);assert.equal(hash(baseBytes),control.completion_sha256);const baseCompletion=JSON.parse(baseBytes);assert.equal(baseCompletion.run_id,control.run_id);
    const actualRun=await smallDownload({...params,method:'spectral-v1',file:'run.json'},control.downloads['run.json']);assert.equal(hash(actualRun),baseCompletion.run.sha256);assert.equal(actualRun.length,baseCompletion.run.bytes);
    const methodCompletion=JSON.parse(await smallDownload({...params,method:'java-reflections-v1',file:'method-completion.json'},control.method_downloads['java-reflections-v1']['method-completion.json']));assert.equal(methodCompletion.base_run_id,control.run_id);
    for(const method of ['spectral-v1','java-reflections-v1'])for(const file of ['run.json','coverage.json','country_eci.parquet','discipline_pci.parquet',...(method==='java-reflections-v1'?['method-audit.json','method-sensitivity.json']:[])]){
      const artifact=control.method_downloads?.[method]?.[file]||control.downloads[file];await smallDownload({...params,method,file},artifact);
    }
    for(const method of ['spectral-v1','java-reflections-v1'])for(const [file,artifact] of Object.entries({...control.downloads,...control.method_downloads?.[method]})){
      if(artifact.path)continue;
      const url='/download?'+new URLSearchParams({...params,method,file}),[a,b]=await Promise.all([request(edge,url,{method:'HEAD'}),request(native,url,{method:'HEAD'})]);assert.equal(a.status,302,url);assert.equal(b.status,200,url);assert.equal(a.headers.get('Location'),artifact.url,url);assert.equal(Number(b.headers.get('Content-Length')),artifact.bytes,url);assert.equal(await a.text(),'');assert.equal(await b.text(),'');for(const r of [a,b])assert.equal(r.headers.get('X-OpenAlex-Country-Version'),version,url);
      const methodArtifact=control.method_downloads?.[method]?.[file],completion=methodArtifact?methodCompletion:baseCompletion,filename=artifact.filename||file,found=completion.artifacts.find(x=>path.posix.basename(x.path)===filename)||completion.artifacts.find(x=>path.posix.basename(x.path)===file);
      assert.ok(found,`completion artifact missing ${url}`);assert.equal(found.bytes,artifact.bytes,url);assert.equal(found.sha256,artifact.sha256,url);
      if(process.env.CHECK_RELEASE_HEAD==='1'){const release=await fetch(artifact.url,{method:'HEAD',signal:AbortSignal.timeout(60000)});assert.equal(release.status,200,artifact.url);assert.equal(Number(release.headers.get('Content-Length')),artifact.bytes,artifact.url);report.release_head_checks++;}
      report.large_download_heads++;
    }
    for(const base of [edge,native]){const head=await request(base,url,{method:'HEAD'});assert.equal(head.status,200);assert.equal(head.headers.get('X-OpenAlex-Country-Version'),version);assert.equal(await head.text(),'');report.head_cases++;}
  }
  for(const corpus of ['core','all'])for(const base of [edge,native]){const [a,b]=await Promise.all([json(base,`/api/meta?corpus=${corpus}`),json(base,`/api/meta?corpus=${corpus}&country_version=enriched`)]);assert.deepEqual(canonicalMeta(a.body),canonicalMeta(b.body));assert.equal(a.version,'enriched');report.default_cases++;}
  const queue=[...jobs,...allCountries];let cursor=0;await Promise.all(Array.from({length:concurrency},async()=>{while(cursor<queue.length){const i=cursor++;await compare(queue[i]);if(report.cases%200===0)console.log(JSON.stringify({status:'running',cases:report.cases,of:queue.length,point_values:report.point_values}));}}));
  for(const params of csvJobs)await compare(params,true);
  for(const tail of ['country_version=','country_version=bogus','country_version=original&country_version=enriched'])for(const endpoint of ['/api/meta','/api/series','/download']){
    const url=endpoint+'?'+tail,[a,b]=await Promise.all([json(edge,url),json(native,url)]);assert.equal(a.status,400,url);assert.equal(b.status,400,url);assert.deepEqual(a.body,b.body,url);report.invalid_cases++;
  }
  for(const url of ['/api/catalog?country_version=original','/api/meta?country_version=original&corpus=bad','/api/meta?country_version=enriched&corpus=all&method=spectral-v1','/download?country_version=original&file=../secret','/download?country_version=enriched&file=run.json&file=run.json']){const [a,b]=await Promise.all([json(edge,url),json(native,url)]);assert.equal(a.status,400,url);assert.equal(b.status,400,url);assert.deepEqual(a.body,b.body,url);report.invalid_cases++;}
  assert.ok(report.version_difference_cases>0,'No observed scientific value/rank changes between the two actual country versions');report.version_difference_witnesses=differenceWitness;report.status='passed';
}catch(error){report.status='failed';report.failure=String(error.stack||error).slice(0,9000);console.error(report.failure);process.exitCode=1;}
finally{report.completed_at_utc=new Date().toISOString();await fs.mkdir(path.dirname(output),{recursive:true});await fs.writeFile(output,JSON.stringify(report,null,2)+'\n');console.log(JSON.stringify(report,null,2));}
