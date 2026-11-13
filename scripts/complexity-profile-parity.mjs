// Real-model profile parity against Go. No scientific computation happens here.
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import {createHash} from 'node:crypto';

const edge=(process.env.PAGES_URL||'http://127.0.0.1:8798').replace(/\/$/,''),native=(process.env.GO_URL||'http://192.168.1.229:8898').replace(/\/$/,'');
const root=path.resolve(process.env.STATIC_ROOT||'cloudflare/data'),output=path.resolve(process.env.PARITY_OUTPUT||'research/longtail-patch-v2/parity/pages-parity.json');
const workers=Number(process.env.PARITY_WORKERS||6);assert.ok(Number.isInteger(workers)&&workers>=1&&workers<=12);
const report={status:'running',started_at_utc:new Date().toISOString(),edge,native,json_cases:0,point_values:0,csv_cases:0,metadata_cases:0,head_downloads:0,byte_downloads:0,get_missing_content_length:0,get_chunked_transfer:0,invalid_cases:0,default_cases:0,profiles:0,datasets:0,basic_count_invariance_cases:0,method_support_invariance_cases:0,policy:'Source/query/profile identities, context, support/masks, values, statuses, global ranks/universes and series order exact. Metadata ID sets canonicalized. CSV text/order exact, numeric Float64 exact. Profile download bodies compared by native raw SHA, including gzip-decoded bytes; all profile download HEADs require exact entity Content-Length; GET may be chunked/missing Content-Length, but any supplied length and every actual body byte count/SHA are exact.'};
const hash=b=>createHash('sha256').update(b).digest('hex');
async function request(base,url,options={}){return fetch(base+url,{redirect:'manual',signal:AbortSignal.timeout(60000),...options});}
async function get(base,url){const r=await request(base,url),text=await r.text();let body;try{body=JSON.parse(text);}catch{throw new Error(`${base}${url}: HTTP${r.status} non-JSON: ${text.slice(0,800)}`);}return {status:r.status,body,country:r.headers.get('X-OpenAlex-Country-Version'),profile:r.headers.get('X-OpenAlex-Complexity-Profile')};}
function canonical(meta){const m=structuredClone(meta);for(const key of ['countries','methods','scenarios','country_versions','complexity_profiles'])if(m[key])m[key].sort((a,b)=>a.id.localeCompare(b.id));for(const key of ['downloads','profile_model_downloads'])if(m[key])m[key].sort();if(m.layers){m.layers.sort((a,b)=>`${a.scheme}/${a.level}`.localeCompare(`${b.scheme}/${b.level}`));for(const layer of m.layers)layer.labels.sort((a,b)=>a.id.localeCompare(b.id));}return m;}
function parseCSV(text){const rows=[];let row=[],cell='',quoted=false;for(let i=0;i<text.length;i++){const c=text[i];if(c==='"'){if(quoted&&text[i+1]==='"'){cell+='"';i++;}else quoted=!quoted;}else if(c===','&&!quoted){row.push(cell);cell='';}else if(c==='\n'&&!quoted){row.push(cell);rows.push(row);row=[];cell='';}else if(c!=='\r'||quoted)cell+=c;}assert.equal(quoted,false);return rows;}
let site,manifests;const counts=new Map(),supports=new Map();
function datasetFor(q){return q.complexity_profile==='baseline-v1'?null:manifests.get(q.complexity_profile).datasets[q.country_version][q.corpus];}
async function compare(q,asCSV=false){
  const url='/api/series'+(asCSV?'.csv':'')+'?'+new URLSearchParams(q);
  if(asCSV){
    const [a,b]=await Promise.all([request(edge,url),request(native,url)]);assert.equal(a.status,200,url);assert.equal(b.status,200,url);const ar=parseCSV(await a.text()),br=parseCSV(await b.text());assert.deepEqual(ar[0],br[0],url);assert.equal(ar.length,br.length,url);
    const numeric=new Set(['year','value','rank','rank_universe','country_iterations','discipline_iterations']);
    for(const field of ['complexity_profile','profile_applies','profile_run_id'])assert.ok(ar[0].includes(field));
    for(let i=1;i<ar.length;i++)for(let j=0;j<ar[0].length;j++)if(numeric.has(ar[0][j])&&ar[i][j]!==''&&br[i][j]!=='')assert.equal(Number(ar[i][j]),Number(br[i][j]),`CSV ${url} ${i}/${j}`);else assert.equal(ar[i][j],br[i][j],`CSV ${url} ${i}/${j}`);report.csv_cases++;return;
  }
  const [a,b]=await Promise.all([get(edge,url),get(native,url)]);assert.equal(a.status,200,url);assert.deepEqual(a,b,url);
  const body=a.body,applies=q.metric==='eci'||q.metric==='pci',control=site.country_versions[q.country_version][q.corpus],profile=site.catalog.complexity_profiles.find(p=>p.id===q.complexity_profile),run=applies?(profile.baseline?control.run_id:datasetFor(q).profile_run_id):'';
  assert.equal(body.query.complexity_profile,profile.id);assert.equal(body.query.country_version,q.country_version);assert.equal(body.profile_applies,applies);assert.equal(body.profile_run_id,run);assert.equal(body.method.source_run_id,control.run_id);assert.equal(body.model_context.source_run_id,control.run_id);assert.equal(body.model_context.source_completion_sha256,control.completion_sha256);assert.equal(body.model_context.direction_policy,profile.direction_policy);assert.equal(a.profile,profile.id);
  report.json_cases++;report.point_values+=body.series.reduce((n,s)=>n+s.points.length,0);
  if(!applies){
    const key=JSON.stringify({...q,complexity_profile:''}),value=hash(JSON.stringify(body.series));if(counts.has(key)){assert.equal(value,counts.get(key),'Basic count/share/RCA changed with profile');report.basic_count_invariance_cases++;}else counts.set(key,value);assert.deepEqual(body.model_context.years,[]);
  }else if(q.metric==='eci'&&!profile.baseline){
    const key=JSON.stringify({...q,method:''}),value=hash(JSON.stringify(body.series.map(s=>s.points.map(p=>[p.support||null,p.filter_reasons||null]))));if(supports.has(key)){assert.equal(value,supports.get(key),'Methods used different country eligibility/support');report.method_support_invariance_cases++;}else supports.set(key,value);
  }
}
async function download(q,name,method,body=false){
  const dataset=datasetFor(q),control=site.country_versions[q.country_version][q.corpus],artifact=dataset?(dataset.downloads[method]?.[name]||dataset.downloads.shared?.[name]):(control.method_downloads?.[method]?.[name]||control.downloads[name]);assert.ok(artifact,`Missing selected download ${name}`);
  const url='/download?'+new URLSearchParams({...q,file:name,method}),[a,b]=await Promise.all([request(edge,url,{method:body?'GET':'HEAD'}),request(native,url,{method:body?'GET':'HEAD'})]);
  if(!artifact.path){assert.equal(a.status,302,url);assert.equal(b.status,200,url);assert.equal(a.headers.get('Location'),artifact.url,url);assert.equal(Number(b.headers.get('Content-Length')),artifact.bytes,url);report.head_downloads++;return;}
  assert.equal(a.status,200,url);assert.equal(b.status,200,url);for(const r of [a,b]){assert.equal(r.headers.get('X-OpenAlex-Country-Version'),q.country_version);assert.equal(r.headers.get('X-OpenAlex-Complexity-Profile'),q.complexity_profile);if(dataset){assert.equal(r.headers.get('X-Artifact-SHA256'),artifact.sha256,url);assert.equal(r.headers.get('X-OpenAlex-Profile-Run-ID'),dataset.profile_run_id,url);}}
  for(const r of [a,b]){const length=r.headers.get('Content-Length');if(!body)assert.notEqual(length,null,`HEAD missing entity length ${url}`);if(length!==null)assert.equal(Number(length),artifact.bytes,url);else report.get_missing_content_length++;if(body&&r.headers.get('Transfer-Encoding')?.toLowerCase().includes('chunked'))report.get_chunked_transfer++;}assert.equal(a.headers.get('Content-Disposition'),b.headers.get('Content-Disposition'),url);
  if(body){const [aa,bb]=await Promise.all([a.arrayBuffer(),b.arrayBuffer()]);for(const bytes of [aa,bb]){assert.equal(bytes.byteLength,artifact.bytes,url);assert.equal(hash(new Uint8Array(bytes)),artifact.sha256,url);}report.byte_downloads++;}else{assert.equal(await a.text(),'');assert.equal(await b.text(),'');report.head_downloads++;}
}
try{
  site=JSON.parse(await fs.readFile(path.join(root,'data/site.json'),'utf8'));assert.ok(site.complexity_profile_manifests,'Need completed static profile export');
  const profiles=site.catalog.complexity_profiles;report.profiles=profiles.length;assert.equal(site.catalog.default_complexity_profile,'p1000-c1000-x5-d3-v1');
  manifests=new Map();for(const p of profiles)if(!p.baseline){const ref=site.complexity_profile_manifests[p.id],raw=await fs.readFile(path.join(root,ref.path.slice(1)));assert.equal(hash(raw),ref.sha256);const m=JSON.parse(raw);assert.equal(m.direction_policy,'baseline-pooled-common-year-sign-v1');manifests.set(p.id,m);}
  const catalogs=await Promise.all([get(edge,'/api/catalog'),get(native,'/api/catalog')]);assert.equal(catalogs[0].status,200);assert.deepEqual(canonical(catalogs[0].body),canonical(catalogs[1].body));
  const jobs=[],csvJobs=[],downloadHeads=[],downloadBodies=[];
  for(const version of ['original','enriched'])for(const corpus of ['core','all']){
    report.datasets++;const [a,b]=await Promise.all([get(edge,`/api/meta?country_version=${version}&corpus=${corpus}`),get(native,`/api/meta?country_version=${version}&corpus=${corpus}`)]);assert.equal(a.status,200);assert.deepEqual(canonical(a.body),canonical(b.body));report.metadata_cases++;
    const meta=a.body,first=meta.layers[0],countryIDs=meta.countries.map(c=>c.id).join(','),selectedCountries=['CN','US','GB','AG','EH','SG','JP','IN','RU'].filter(id=>meta.countries.some(c=>c.id===id)).join(',');
    for(const profile of profiles){
      const base={country_version:version,corpus,complexity_profile:profile.id,start:'2000',end:'2026'};
      for(const layer of meta.layers){const labels=[...layer.labels].sort((x,y)=>x.id.localeCompare(y.id)),ids=[labels[0].id,labels[Math.floor(labels.length/2)].id].join(',');for(const method of meta.methods)for(const basis of ['publications','citations'])for(const metric of ['eci','pci'])jobs.push({...base,scheme:layer.scheme,level:layer.level,method:method.id,basis,metric,disciplines:ids,countries:selectedCountries});}
      for(const metric of ['publications','citations','share','rca'])for(const basis of (['share','rca'].includes(metric)?['publications','citations']:['publications']))jobs.push({...base,scheme:first.scheme,level:first.level,method:'java-reflections-v1',metric,basis,disciplines:first.labels[0].id,countries:'CN,US,GB'});
      for(const method of meta.methods)csvJobs.push({...base,scheme:'concepts',level:'0',method:method.id,basis:'citations',metric:'eci',countries:'CN,US,AG,EH'});
      for(const method of meta.methods)for(const name of (profile.baseline?['eci.json','country_eci.parquet','discipline_pci.parquet']:['eci.json','country_eci.parquet','discipline_pci.parquet','model_diagnostics.json','profile-manifest.json','profile-direction.json']))downloadHeads.push({q:{country_version:version,corpus,complexity_profile:profile.id},name,method:method.id});
      if(profile.baseline||profile.id===site.catalog.default_complexity_profile){for(const layer of meta.layers)for(const method of meta.methods)for(const basis of ['publications','citations'])jobs.push({...base,scheme:layer.scheme,level:layer.level,method:method.id,basis,metric:'eci',countries:countryIDs});}
      if(profile.id===site.catalog.default_complexity_profile)for(const method of meta.methods)for(const name of ['eci.json','country_eci.parquet','discipline_pci.parquet','profile-direction.json'])downloadBodies.push({q:{country_version:version,corpus,complexity_profile:profile.id},name,method:method.id});
      if(profile.baseline)for(const method of meta.methods)downloadBodies.push({q:{country_version:version,corpus,complexity_profile:profile.id},name:'country_eci.parquet',method:method.id});
    }
    const parameters={country_version:version,corpus,scheme:first.scheme,level:first.level,metric:'eci',countries:'CN,US',start:'2025',end:'2025'},url='/api/series?'+new URLSearchParams(parameters);
    for(const origin of [edge,native]){const d=await get(origin,url),explicit=await get(origin,url+'&complexity_profile='+meta.default_complexity_profile);assert.deepEqual(d.body,explicit.body);report.default_cases++;}
  }
  async function runQueue(queue,fn,label){let cursor=0,completed=0,stopped=false;const results=await Promise.allSettled(Array.from({length:workers},async()=>{while(!stopped&&cursor<queue.length){const row=queue[cursor++];try{await fn(row);}catch(error){stopped=true;throw error;}completed++;if(completed%200===0)console.log(JSON.stringify({stage:label,completed,total:queue.length}));}}));const failed=results.find(r=>r.status==='rejected');if(failed)throw failed.reason;}
  await runQueue(jobs,q=>compare(q),'json');for(const q of csvJobs)await compare(q,true);
  await runQueue(downloadHeads,item=>download(item.q,item.name,item.method),'download-head');await runQueue(downloadBodies,item=>download(item.q,item.name,item.method,true),'download-body');
  const invalid=['/api/series?complexity_profile=','/api/series?complexity_profile=bogus','/api/series?complexity_profile=baseline-v1&complexity_profile=baseline-v1','/download?file=eci.json&complexity_profile=','/download?file=eci.json&complexity_profile=bogus','/download?file=method-sensitivity.json&complexity_profile=p1000-c1000-x5-d3-v1','/api/meta?complexity_profile=baseline-v1','/api/catalog?complexity_profile=baseline-v1'];
  for(const url of invalid){const [a,b]=await Promise.all([get(edge,url),get(native,url)]);assert.equal(a.status,400,url);assert.equal(b.status,400,url);assert.deepEqual(a.body,b.body,url);report.invalid_cases++;}
  report.status='passed';
}catch(error){report.status='failed';report.failure=String(error.stack||error).slice(0,10000);console.error(report.failure);process.exitCode=1;}
finally{report.completed_at_utc=new Date().toISOString();await fs.mkdir(path.dirname(output),{recursive:true});await fs.writeFile(output,JSON.stringify(report,null,2)+'\n');console.log(JSON.stringify(report,null,2));}
