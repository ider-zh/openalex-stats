import {test} from 'node:test';
import assert from 'node:assert/strict';
import {gzipSync,gunzipSync} from 'node:zlib';
import {createHash} from 'node:crypto';
import {handle,parseQuery,goFloat} from './worker.mjs';
let serial=0;
function matrix(records,totals=[[10,20],[0,0]],countries=2){const years=2,b=Buffer.alloc(16+(countries+1)*years*16);b.write('OAQ1');b.writeUInt16LE(2000,4);b.writeUInt16LE(years,6);b.writeUInt16LE(countries,8);for(let y=0;y<years;y++)for(let basis=0;basis<2;basis++)b.writeDoubleLE(totals[y][basis],16+y*16+basis*8);for(const [c,y,p,v] of records){const offset=16+years*16+(c*years+y)*16;b.writeDoubleLE(p,offset);b.writeDoubleLE(v,offset+8);}return gzipSync(b);}
function fixture(){
  const origin=`https://fixture${++serial}.example`,store=new Map(),requests=[];
  const meta={corpus:'core',year_start:2000,year_end:2001,default_year_end:2001,partial_years:[],eci_scenario_id:'main',max_series:48,scenarios:[{id:'main'}],countries:[{id:'CN',display_name:'China'},{id:'US',display_name:'United States'}],layers:[{scheme:'topics',level:'field',labels:Array.from({length:48},(_,i)=>({id:`f${i+1}`,display_name:`Subject ${i+1}`}))}],methods:[{id:'spectral-v1'},{id:'java-reflections-v1'}],downloads:['run.json','statistics.parquet']};
  const control={prefix:'/data/core-run',meta,templates:{},models:{},downloads:{'run.json':{path:'/data/core-run/artifacts/run.json',sha256:'abc'},'statistics.parquet':{url:'https://github.com/ider-zh/openalex-stats/releases/download/v1/core-statistics.parquet'}}};
  for(const method of meta.methods)for(const metric of ['publications','citations','share','rca','eci','pci'])for(const basis of ['publications','citations'])control.templates[[method.id,'main','topics','field',metric,basis].join('/')]={definition:'fixture',attribution:'fixture',counting:{country_mode:'fractional'},method:{id:method.id,country_iterations:method.id==='spectral-v1'?0:16,discipline_iterations:method.id==='spectral-v1'?0:17,source_run_id:'run'},method_applies:['eci','pci'].includes(metric)};
  for(const [method,value] of [['spectral-v1',1.2],['java-reflections-v1',.8]]){const path=`/data/core-run/models/${method}.json.gz`;control.models[[method,'main','topics','field','publications'].join('/')]=path;store.set(path,gzipSync(JSON.stringify({countries:[[[value,'ok',1,2],[null,'disconnected',null,0]],[[-value,'ok',2,2],[null,'disconnected',null,0]]],disciplines:meta.layers[0].labels.map((_,i)=>[[i?-.7:.7,'ok',i?2:1,48],[null,'disconnected',null,0]])})));}
  for(let i=0;i<48;i++)store.set(`/data/core-run/counts/0/0/${i}.bin.gz`,matrix(i===0?[[0,0,2,4],[1,0,6,12]]:i===1?[[0,0,2,0],[1,0,0,4]]:[],i===0?[[8,16],[0,0]]:i===1?[[2,4],[0,0]]:[[0,0],[0,0]]));
  store.set('/data/core-run/counts/0/0/margins.bin.gz',matrix([[0,0,4,4],[1,0,6,16]]));store.set('/data/site.json',Buffer.from(JSON.stringify({version:'openalex-static-query-v1',catalog:{default_corpus:'core',datasets:[{id:'core'}]},datasets:{core:control}})));store.set('/data/core-run/artifacts/run.json',Buffer.from('{"run_id":"core-run"}\n'));
  const env={ASSETS:{async fetch(input){const request=input instanceof Request?input:new Request(input),path=new URL(request.url).pathname;requests.push(path);const b=store.get(path);return b?new Response(request.method==='HEAD'?null:b,{headers:{'Content-Type':'application/octet-stream'}}):new Response('not found',{status:404});}}};
  return {origin,meta,control,env,requests,store,async query(values={}){const p=new URLSearchParams({scheme:'topics',level:'field',disciplines:'f1,f2',countries:'CN,US',start:'2000',end:'2001',...values});const r=await handle(new Request(`${origin}/api/series?${p}`),env);return {status:r.status,body:await r.json()};}};
}

test('ECI accepts the complete known-country universe beyond 12/48, without truncation or reranking',async()=>{
  const f=fixture();
  // Synthetic fixture codes exercise the API cardinality boundary, not real countries.
  for(let i=0;i<60;i++){const id=String.fromCharCode(65+Math.floor(i/26))+String.fromCharCode(65+i%26);f.meta.countries.push({id,display_name:`Fixture ${id}`});}
  const ids=f.meta.countries.map(c=>c.id),universe=ids.length;
  f.store.set('/data/site.json',Buffer.from(JSON.stringify({version:'openalex-static-query-v1',catalog:{default_corpus:'core',datasets:[{id:'core'}]},datasets:{core:f.control}})));
  for(const method of f.meta.methods){const data={countries:ids.map((_,i)=>[[universe-i,'ok',i+1,universe],[null,'inactive',null,0]]),disciplines:[]};f.store.set(`/data/core-run/models/${method.id}.json.gz`,gzipSync(JSON.stringify(data)));}
  for(const method of f.meta.methods){
    const all=await f.query({metric:'eci',disciplines:'f1',countries:ids.join(','),method:method.id});
    assert.equal(all.status,200);assert.equal(all.body.series.length,universe);
    assert.deepEqual(all.body.series.map(s=>s.id),ids);assert.equal(all.body.series.at(-1).points[0].rank,universe);
    const single=await f.query({metric:'eci',disciplines:'f1',countries:ids.at(-1),method:method.id});
    assert.equal(single.body.series[0].points[0].rank,universe);
    assert.equal((await f.query({metric:'eci',disciplines:'f1',countries:ids.concat(ids).join(','),method:method.id})).body.series.length,universe);
  }
  assert.equal((await f.query({metric:'publications',disciplines:'f1',countries:ids.join(',')})).status,400);
  assert.equal((await f.query({metric:'eci',disciplines:'f1',countries:'ZZ'})).status,400);
  const meta=await handle(new Request(`${f.origin}/api/meta`),f.env);assert.equal((await meta.json()).max_eci_countries,universe);
});
test('country × discipline counts, exact shares/RCA and legacy single selection',async()=>{const f=fixture(),r=await f.query();assert.equal(r.status,200);assert.deepEqual(r.body.series.map(s=>s.id),['CN::f1','CN::f2','US::f1','US::f2']);assert.deepEqual(r.body.series.map(s=>s.points[0].value),[2,2,6,0]);assert.equal((await f.query({metric:'share'})).body.series[0].points[0].value,.25);assert.equal((await f.query({metric:'rca'})).body.series[0].points[0].value,.625);const q=parseQuery(new URLSearchParams({scheme:'topics',level:'field',discipline:'f1',countries:'CN'}),f.meta);assert.equal(q.discipline,'f1');assert.deepEqual(q.disciplines,['f1']);});
test('ECI per-country, PCI per-global-discipline; methods change only models',async()=>{const f=fixture();let r=await f.query({metric:'eci'});assert.equal(r.body.series.length,2);assert.deepEqual(r.body.series.map(s=>s.discipline),['','']);assert.equal(r.body.series[0].points[0].rank,1);assert.equal(r.body.query.method,'java-reflections-v1');assert.equal(r.body.method.display_name,'小倪方法论');assert.equal(r.body.series[0].points[0].value,.8);assert.equal((await f.query({metric:'eci',method:'spectral-v1'})).body.series[0].points[0].value,1.2);r=await f.query({metric:'pci'});assert.equal(r.body.series.length,2);assert.ok(r.body.series.every(s=>s.country==='GLOBAL'));assert.equal(r.body.query.countries,null);assert.equal((await f.query({metric:'eci',method:'java-reflections-v1'})).body.series[0].points[0].value,.8);assert.deepEqual((await f.query({method:'java-reflections-v1'})).body.series.map(s=>s.points),(await f.query()).body.series.map(s=>s.points));});

test('old immutable metadata is presented with Xiao Ni name/default without rewriting artifacts',async()=>{
  const f=fixture(),site=JSON.parse(f.store.get('/data/site.json'));
  site.datasets.core.meta.eci_default_method='spectral-v1';site.datasets.core.meta.methods[1].display_name='Java 反射法·修正版';
  const key='java-reflections-v1/main/topics/field/eci/publications';site.datasets.core.templates[key].method.display_name='Java 反射法·修正版';site.datasets.core.templates[key].definition='知识复杂度方法：Java 反射法·修正版。';
  const original=Buffer.from(JSON.stringify(site));f.store.set('/data/site.json',original);
  const meta=await (await handle(new Request(`${f.origin}/api/meta`),f.env)).json();assert.equal(meta.eci_default_method,'java-reflections-v1');assert.equal(meta.methods[1].display_name,'小倪方法论');
  const r=await f.query({metric:'eci'});assert.equal(r.body.method.display_name,'小倪方法论');assert.equal(r.body.definition,'知识复杂度方法：小倪方法论。');assert.equal((await f.query({metric:'eci',method:'spectral-v1'})).body.query.method,'spectral-v1');assert.deepEqual(f.store.get('/data/site.json'),original);
  const baseMeta={...f.meta,methods:[{id:'spectral-v1'}]};assert.equal(parseQuery(new URLSearchParams({scheme:'topics',level:'field',discipline:'f1',countries:'CN'}),baseMeta).method,'spectral-v1');
});
test('invalid and excessive selectors reject rather than truncate',async()=>{const f=fixture();for(const query of [{countries:'ZZ'},{disciplines:'bad'},{disciplines:'f1',discipline:'f1'},{level:'wrong'},{basis:'bad'},{method:'bad'},{start:'2000.5'},{end:'1999'}])assert.equal((await f.query(query)).status,400);assert.equal((await f.query({disciplines:f.meta.layers[0].labels.map(d=>d.id).join(',')})).status,400);const url=`${f.origin}/api/series?scheme=topics&level=field&discipline=f1&countries=CN&metric=publications&metric=eci`;assert.equal((await handle(new Request(url),f.env)).status,400);});
test('cold 48-curve RCA uses at most 50 static subrequests',async()=>{const f=fixture(),r=await f.query({countries:'CN',metric:'rca',disciplines:f.meta.layers[0].labels.map(d=>d.id).join(',')});assert.equal(r.status,200);assert.equal(r.body.series.length,48);assert.equal(f.requests.length,50);});
test('CSV, download allowlist, HEAD and method/security headers',async()=>{const f=fixture(),r=await handle(new Request(`${f.origin}/api/series.csv?scheme=topics&level=field&discipline=f1&countries=CN&start=2000&end=2000`),f.env);assert.equal(r.status,200);assert.match(await r.text(),/CN,2000,2,ok,false,core/);assert.equal(r.headers.get('X-OpenAlex-Corpus'),'core');assert.ok(r.headers.has('Content-Security-Policy'));const small=await handle(new Request(`${f.origin}/download?file=run.json`),f.env);assert.equal(small.status,200);assert.equal(small.headers.get('X-Artifact-SHA256'),'abc');assert.equal((await handle(new Request(`${f.origin}/download?file=../secret`),f.env)).status,400);const big=await handle(new Request(`${f.origin}/download?file=statistics.parquet`),f.env);assert.equal(big.status,302);assert.match(big.headers.get('Location'),/^https:\/\/github.com\//);const head=await handle(new Request(`${f.origin}/api/meta`,{method:'HEAD'}),f.env);assert.equal(await head.text(),'');assert.equal((await handle(new Request(`${f.origin}/api/meta`,{method:'POST'}),f.env)).status,405);});
test('Go shortest-float CSV notation keeps exponent boundaries',()=>{assert.equal(goFloat(1e6),'1e+06');assert.equal(goFloat(.00001),'1e-05');assert.equal(goFloat(.0001),'0.0001');assert.equal(goFloat(146958),'146958');assert.equal(goFloat(null),'');});

function countryVersionFixture(){
  const f=fixture(),versions={original:{},enriched:{}},base=[...f.store];
  let marker=0;
  for(const version of ['original','enriched'])for(const corpus of ['core','all']){
    marker++;const prefix=`/data/${version}-${corpus}`,control=JSON.parse(JSON.stringify(f.control).replaceAll('/data/core-run',prefix));
    control.meta.corpus=corpus;control.meta.quality={identified:marker};control.run_id=`${version}-${corpus}-run`;
    control.downloads['run.json'].sha256=`${version}-${corpus}-hash`;
    control.downloads['statistics.parquet'].url=`https://github.com/ider-zh/openalex-stats/releases/download/${version}/${corpus}-statistics.parquet`;
    for(const template of Object.values(control.templates))template.method.source_run_id=control.run_id;
    for(const [oldPath,bytes] of base){
      if(!oldPath.startsWith('/data/core-run/'))continue;
      const newPath=oldPath.replace('/data/core-run',prefix);
      if(oldPath.includes('/counts/')){
        const raw=gunzipSync(bytes);for(let offset=16;offset<raw.length;offset+=8)raw.writeDoubleLE(raw.readDoubleLE(offset)*marker,offset);f.store.set(newPath,gzipSync(raw));
      }else if(oldPath.includes('/models/')){
        const model=JSON.parse(gunzipSync(bytes));
        for(const kind of ['countries','disciplines'])for(const entity of model[kind])if(entity[0][0]!=null){entity[0][0]+=marker;entity[0][2]=version==='enriched'?2:1;entity[0][3]=2;}
        f.store.set(newPath,gzipSync(JSON.stringify(model)));
      }else f.store.set(newPath,Buffer.from(JSON.stringify({run_id:control.run_id})));
    }
    versions[version][corpus]=control;
  }
  const catalog={default_corpus:'core',datasets:[{id:'core'},{id:'all'}],default_country_version:'enriched',country_versions:[{id:'original',display_name:'原始版',version:'snapshot-20260626-original'},{id:'enriched',display_name:'国家补全版',version:'country-enriched-v3-r3'}]};
  f.store.set('/data/site.json',Buffer.from(JSON.stringify({version:'openalex-static-query-v1',catalog,datasets:versions.enriched,country_versions:versions})));
  return {...f,versions};
}

test('country versions select full independent corpus controls, metadata, models and immutable global ranks',async()=>{
  const f=countryVersionFixture();
  const catalog=await (await handle(new Request(`${f.origin}/api/catalog`),f.env)).json();assert.equal(catalog.default_country_version,'enriched');assert.deepEqual(catalog.country_versions.map(x=>x.id),['original','enriched']);
  const defaultResult=await f.query({disciplines:'f1',countries:'CN',end:'2000'});assert.equal(defaultResult.body.query.country_version,'enriched');assert.equal(defaultResult.body.series[0].points[0].value,6);
  for(const version of ['enriched','original','enriched'])for(const corpus of ['core','all']){
    const marker=version==='original'?(corpus==='core'?1:2):(corpus==='core'?3:4);
    const r=await f.query({country_version:version,corpus,disciplines:'f1',countries:'CN',end:'2000'});assert.equal(r.status,200);assert.equal(r.body.query.country_version,version);assert.equal(r.body.query.corpus,corpus);assert.equal(r.body.series[0].points[0].value,2*marker);
    const response=await handle(new Request(`${f.origin}/api/meta?corpus=${corpus}&country_version=${version}`),f.env),meta=await response.json();assert.equal(meta.country_version,version);assert.equal(meta.country_version_info.id,version);assert.equal(meta.quality.identified,marker);assert.equal(response.headers.get('X-OpenAlex-Country-Version'),version);
    for(const method of ['spectral-v1','java-reflections-v1']){
      const model=await f.query({country_version:version,corpus,metric:'eci',disciplines:'f1',countries:'CN',method,end:'2000'});assert.equal(model.body.series[0].points[0].value,(method==='spectral-v1'?1.2:.8)+marker);assert.equal(model.body.series[0].points[0].rank,version==='enriched'?2:1);assert.equal(model.body.method.source_run_id,`${version}-${corpus}-run`);
      const pci=await f.query({country_version:version,corpus,metric:'pci',disciplines:'f1',method,end:'2000'});assert.equal(pci.body.series[0].country,'GLOBAL');assert.equal(pci.body.series[0].points[0].rank,version==='enriched'?2:1);
    }
  }
});

test('country version CSV, download bytes, redirect and HEAD remain selected-version bound',async()=>{
  const f=countryVersionFixture();
  for(const version of ['original','enriched']){
    const csv=await handle(new Request(`${f.origin}/api/series.csv?country_version=${version}&corpus=all&scheme=topics&level=field&discipline=f1&countries=CN&start=2000&end=2000`),f.env),text=await csv.text();assert.equal(csv.headers.get('X-OpenAlex-Country-Version'),version);const csvRows=text.trim().split('\n').map(row=>row.split(',')),versionIndex=csvRows[0].indexOf('country_version');assert.ok(versionIndex>=0);assert.equal(csvRows[1][versionIndex],version);
    const small=await handle(new Request(`${f.origin}/download?country_version=${version}&corpus=all&file=run.json`),f.env);assert.deepEqual(await small.json(),{run_id:`${version}-all-run`});assert.equal(small.headers.get('X-Artifact-SHA256'),`${version}-all-hash`);
    const big=await handle(new Request(`${f.origin}/download?country_version=${version}&corpus=all&file=statistics.parquet`),f.env);assert.equal(big.status,302);assert.match(big.headers.get('Location'),new RegExp(`/download/${version}/all-statistics.parquet$`));assert.equal(big.headers.get('X-OpenAlex-Country-Version'),version);
    const head=await handle(new Request(`${f.origin}/api/meta?country_version=${version}`,{method:'HEAD'}),f.env);assert.equal(head.status,200);assert.equal(await head.text(),'');assert.equal(head.headers.get('X-OpenAlex-Country-Version'),version);
  }
});

test('unknown, empty, duplicate and unavailable country versions reject consistently',async()=>{
  const f=countryVersionFixture();
  for(const tail of ['country_version=bogus','country_version=','country_version=original&country_version=enriched'])for(const endpoint of ['/api/meta','/api/series','/download'])assert.equal((await handle(new Request(`${f.origin}${endpoint}?${tail}`),f.env)).status,400);
  const legacy=fixture(),meta=await (await handle(new Request(`${legacy.origin}/api/meta?country_version=original`),legacy.env)).json();assert.equal(meta.country_version,'original');assert.equal(meta.country_version_info.version,'snapshot-20260626-original');assert.equal((await handle(new Request(`${legacy.origin}/api/meta?country_version=enriched`),legacy.env)).status,400);
  const catalog=await (await handle(new Request(`${legacy.origin}/api/catalog`),legacy.env)).json();assert.equal(catalog.default_country_version,'original');assert.deepEqual(catalog.country_versions,[meta.country_version_info]);
});

function profileFixture(){
  const f=countryVersionFixture(),site=JSON.parse(f.store.get('/data/site.json'));
  const baseline={id:'baseline-v1',display_name:'不筛选 · 原模型',country_min_publications:0,country_min_citation_events:0,cell_min_support:0,min_diversity:0,engine:'baseline-v1',baseline:true,direction_policy:'published-baseline-v1'};
  const profile={id:'p1000-c1000-x5-d3-v1',display_name:'P1000 · C1000 · X5 · D3',country_min_publications:1000,country_min_citation_events:1000,cell_min_support:5,min_diversity:3,engine:'support-filter-v1',baseline:false,direction_policy:'baseline-pooled-common-year-sign-v1'};
  site.catalog.complexity_profiles=[baseline,profile];site.catalog.default_complexity_profile=profile.id;
  const manifest={profile,direction_policy:'baseline-pooled-common-year-sign-v1',datasets:{original:{},enriched:{}}},rawDownloads=new Map();
  const digest=b=>createHash('sha256').update(b).digest('hex');
  for(const version of ['original','enriched'])for(const corpus of ['core','all']){
    const control=site.country_versions[version][corpus];control.completion_sha256=`${version}-${corpus}-completion`;
    control.meta.scenarios.push({id:'other'});
    for(const [key,template] of Object.entries(control.templates))control.templates[key.replace('/main/','/other/')]=structuredClone(template);
    const profileRun=`profile-${version}-${corpus}`,source=control.run_id,source_completion_sha256=control.completion_sha256;
    const dataset={source_run_id:source,source_completion_sha256,profile_run_id:profileRun,models:{},downloads:{shared:{}}};
    for(const method of ['spectral-v1','java-reflections-v1']){
      const prefix=`/data/profiles/${profile.id}/${version}/${corpus}/${method}`;
      const model={countries:[[[null,'filtered_country_publications',null,0,1,50,2000,0],[1.25,'ok',1,2,0,3000,5000,4]],[[null,'empty',null,0,0,2000,5000,3],[-1.25,'ok',2,2,0,4000,7000,3]]],disciplines:control.meta.layers[0].labels.map((_,i)=>[[null,'empty',null,0],i<2?[i?-.8:.8,'ok',i?2:1,2]:[null,'inactive',null,2]]),model_context:{profile_id:profile.id,profile_run_id:profileRun,source_run_id:source,source_completion_sha256,algorithm:'country-scale-cell-zero-monotone-diversity-v1',direction_policy:'baseline-pooled-common-year-sign-v1',years:[{year:2000,status:'empty',eligible_country_count:1,eligible_discipline_count:3,retained_mass_fraction:.5,closure_passes:1},{year:2001,status:'ok',eligible_country_count:2,eligible_discipline_count:4,retained_mass_fraction:.9,closure_passes:2}]}};
      const modelPath=prefix+'/model.json.gz';f.store.set(modelPath,gzipSync(JSON.stringify(model)));dataset.models[[method,'main','topics','field','publications'].join('/')]=modelPath;
      const raw=Buffer.from('{\n  "source_run_id": "'+source+'",\n  "profile_run_id": "'+profileRun+'",\n  "method": "'+method+'"\n}\n');
      const download=prefix+'/eci.json.gz';f.store.set(download,gzipSync(raw));rawDownloads.set(`${version}/${corpus}/${method}`,raw);
      dataset.downloads[method]={'eci.json':{filename:'eci.json',path:download,encoding:'gzip',bytes:raw.length,sha256:digest(raw)}};
    }
    manifest.datasets[version][corpus]=dataset;
  }
  const manifestPath=`/data/profiles/${profile.id}/manifest.json`;f.store.set(manifestPath,Buffer.from(JSON.stringify(manifest)));site.complexity_profile_manifests={[profile.id]:{path:manifestPath}};
  site.datasets=site.country_versions.enriched;f.store.set('/data/site.json',Buffer.from(JSON.stringify(site)));
  return {...f,site,manifest,profile,baseline,rawDownloads};
}

test('complexity profile defaults, baseline preservation, filtered support and same-network ECI/PCI context',async()=>{
  const f=profileFixture();
  const catalog=await (await handle(new Request(`${f.origin}/api/catalog`),f.env)).json();assert.equal(catalog.default_complexity_profile,f.profile.id);assert.deepEqual(catalog.complexity_profiles,[f.baseline,f.profile]);
  for(const version of ['original','enriched'])for(const corpus of ['core','all'])for(const method of ['spectral-v1','java-reflections-v1']){
    const p={country_version:version,corpus,method,metric:'eci',disciplines:'f1',countries:'CN,US'};
    const {body,status}=await f.query(p);assert.equal(status,200);assert.equal(body.query.complexity_profile,f.profile.id);assert.deepEqual(body.complexity_profile,f.profile);assert.equal(body.profile_applies,true);assert.equal(body.profile_run_id,`profile-${version}-${corpus}`);
    assert.equal(body.model_context.source_run_id,`${version}-${corpus}-run`);assert.equal(body.model_context.years.length,2);assert.equal(body.series[0].points[0].status,'filtered_country_publications');assert.deepEqual(body.series[0].points[0].filter_reasons,['country_publications']);assert.deepEqual(body.series[0].points[1].support,{whole_publications:3000,whole_citations:5000,final_diversity:4});assert.equal(body.series[1].points[1].rank,2);assert.equal(body.series[1].points[1].rank_universe,2);
    const one=(await f.query({...p,countries:'US',start:'2001',end:'2001'})).body;assert.equal(one.series[0].points[0].rank,2);assert.deepEqual(one.model_context.years.map(y=>y.year),[2001]);
    const pci=(await f.query({...p,metric:'pci',disciplines:'f1,f2'})).body;assert.equal(pci.profile_run_id,body.profile_run_id);assert.deepEqual(pci.model_context,body.model_context);assert.equal(pci.series[0].points[1].support,undefined);assert.equal(pci.series[1].points[1].rank,2);
    const baseline=(await f.query({...p,complexity_profile:'baseline-v1'})).body;assert.equal(baseline.profile_run_id,`${version}-${corpus}-run`);assert.deepEqual(baseline.model_context.years,[]);assert.equal(baseline.model_context.algorithm,'baseline-v1');assert.equal(baseline.series[0].points[0].filter_reasons,undefined);assert.equal(baseline.series[0].points[0].support,undefined);assert.notEqual(baseline.series[0].points[0].value,null);
    const unavailable=(await f.query({...p,scenario:'other'})).body;assert.equal(unavailable.profile_applies,true);assert.equal(unavailable.series[0].points[0].status,'model_not_available');assert.deepEqual(unavailable.model_context.years,[]);assert.equal(unavailable.series[0].points[0].support,undefined);
  }
});

test('ordinary counts and RCA keep original values and cold 50-subrequest boundary without reading profile manifests',async()=>{
  const f=profileFixture(),first=await f.query({countries:'CN',disciplines:f.meta.layers[0].labels.map(d=>d.id).join(','),metric:'rca'});
  assert.equal(first.status,200);assert.equal(first.body.profile_applies,false);assert.equal(first.body.profile_run_id,'');assert.deepEqual(first.body.model_context.years,[]);assert.equal(f.requests.length,50);assert.ok(!f.requests.some(p=>p.includes('/profiles/')));
  const filtered=(await f.query({metric:'publications',disciplines:'f1',countries:'CN'})).body,base=(await f.query({metric:'publications',disciplines:'f1',countries:'CN',complexity_profile:'baseline-v1'})).body;assert.deepEqual(filtered.series,base.series);
});

test('profile model downloads decode gzip raw bytes exactly, HEAD is raw-sized, and missing sensitivity never falls back',async()=>{
  const f=profileFixture();
  for(const version of ['original','enriched'])for(const corpus of ['core','all'])for(const method of ['spectral-v1','java-reflections-v1']){
    const url=`${f.origin}/download?`+new URLSearchParams({country_version:version,corpus,method,complexity_profile:f.profile.id,file:'eci.json'}),expected=f.rawDownloads.get(`${version}/${corpus}/${method}`);
    const r=await handle(new Request(url,{headers:{Range:'bytes=0-1'}}),f.env);assert.equal(r.status,200);assert.deepEqual(Buffer.from(await r.arrayBuffer()),expected);assert.equal(Number(r.headers.get('Content-Length')),expected.length);assert.equal(r.headers.get('X-Artifact-SHA256'),createHash('sha256').update(expected).digest('hex'));assert.match(r.headers.get('Content-Disposition'),new RegExp(f.profile.id));assert.match(r.headers.get('Content-Disposition'),new RegExp(method));assert.equal(r.headers.get('X-OpenAlex-Profile-Applies'),'true');
    const head=await handle(new Request(url,{method:'HEAD'}),f.env);assert.equal(head.status,200);assert.equal(await head.text(),'');assert.equal(Number(head.headers.get('Content-Length')),expected.length);
    const sensitivity=await handle(new Request(url.replace('file=eci.json','file=method-sensitivity.json')),f.env);assert.equal(sensitivity.status,400);
    const quantity=await handle(new Request(url.replace('file=eci.json','file=statistics.parquet')),f.env);assert.equal(quantity.status,302);assert.equal(quantity.headers.get('X-OpenAlex-Profile-Applies'),'false');
  }
});

test('unknown, empty, duplicate and source-mismatched profile selectors reject rather than fall back',async()=>{
  const f=profileFixture();for(const value of ['', 'unknown'])assert.equal((await f.query({complexity_profile:value})).status,400);
  const duplicated=await handle(new Request(`${f.origin}/api/series?complexity_profile=baseline-v1&complexity_profile=${f.profile.id}`),f.env);assert.equal(duplicated.status,400);
  const broken=profileFixture();broken.manifest.datasets.enriched.core.source_run_id='wrong-source';broken.store.set(broken.site.complexity_profile_manifests[broken.profile.id].path,Buffer.from(JSON.stringify(broken.manifest)));assert.equal((await broken.query({metric:'eci'})).status,500);
  const oldDirection=profileFixture();delete oldDirection.manifest.direction_policy;oldDirection.store.set(oldDirection.site.complexity_profile_manifests[oldDirection.profile.id].path,Buffer.from(JSON.stringify(oldDirection.manifest)));assert.equal((await oldDirection.query({metric:'eci'})).status,500);
  for(const file of ['constructor','toString','__proto__'])assert.equal((await handle(new Request(`${f.origin}/download?file=${file}`),f.env)).status,400);
  assert.equal((await handle(new Request(`${f.origin}/api/meta?corpus=constructor`),f.env)).status,400);
});

test('cold concurrent requests own their I/O and reuse only completed parsed cache values',async()=>{
  const f=profileFixture();
  const requests=await Promise.all([f.query({metric:'eci'}),f.query({metric:'eci'})]);
  assert.equal(requests[0].status,200);assert.deepEqual(requests[0].body,requests[1].body);
  assert.equal(f.requests.filter(p=>p==='/data/site.json').length,2,'Pending site fetch must not be shared across requests');
  const before=f.requests.length;assert.equal((await f.query({metric:'eci'})).status,200);assert.equal(f.requests.length,before,'Completed plain objects remain cacheable');
});

test('plain HEAD entity length comes from artifact identity when ASSETS reports zero, without changing range lengths',async()=>{
  const f=fixture(),bytes=f.store.get('/data/core-run/artifacts/run.json');f.control.downloads['run.json'].bytes=bytes.length;
  f.store.set('/data/site.json',Buffer.from(JSON.stringify({version:'openalex-static-query-v1',catalog:{default_corpus:'core',datasets:[{id:'core'}]},datasets:{core:f.control}})));
  const original=f.env.ASSETS.fetch;f.env.ASSETS.fetch=async input=>{
    const request=input instanceof Request?input:new Request(input),result=await original(input);
    if(request.method!=='HEAD'||!new URL(request.url).pathname.endsWith('/artifacts/run.json'))return result;
    const headers=new Headers(result.headers);
    if(request.headers.has('Range')){headers.set('Content-Length','2');headers.set('Content-Range',`bytes 0-1/${bytes.length}`);return new Response(null,{status:206,headers});}
    headers.set('Content-Length','0');return new Response(null,{status:200,headers});
  };
  const url=f.origin+'/download?file=run.json',head=await handle(new Request(url,{method:'HEAD'}),f.env);assert.equal(head.status,200);assert.equal(Number(head.headers.get('Content-Length')),bytes.length);assert.equal(await head.text(),'');
  const range=await handle(new Request(url,{method:'HEAD',headers:{Range:'bytes=0-1'}}),f.env);assert.equal(range.status,206);assert.equal(range.headers.get('Content-Length'),'2');
});
