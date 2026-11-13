// Cloudflare Pages advanced-mode adapter. Scientific values and global ranks
// are exported by Go; this module only validates filters and reads lookup data.
const MIME = 'application/json; charset=utf-8';
const CSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'";
const allowed = new Set(['scenario','scheme','level','discipline','disciplines','countries','metric','basis','start','end','method','complexity_profile']);
const cache = new Map();
const siteCache = new Map();
const MAX_CACHE = 16;
const xiaoNiMethod = 'java-reflections-v1';
const baselinePolicy='published-baseline-v1',profilePolicy='baseline-pooled-common-year-sign-v1';
const baselineProfile={id:'baseline-v1',display_name:'不筛选 · 原模型',country_min_publications:0,country_min_citation_events:0,cell_min_support:0,min_diversity:0,engine:'baseline-v1',baseline:true,direction_policy:baselinePolicy};
const supportAlgorithm='country-scale-cell-zero-monotone-diversity-v1';
const reasonNames=['country_publications','country_citations','cell_support','min_diversity','original_zero'];
const own=(object,key)=>object&&Object.hasOwn(object,key)?object[key]:undefined;
function profiles(meta){return meta.complexity_profiles?.length?meta.complexity_profiles:[baselineProfile];}
function defaultProfile(meta){return meta.default_complexity_profile||'baseline-v1';}
function selectedProfile(params,meta,punctuation=''){
  const values=params.getAll('complexity_profile');
  if(values.length>1)fail('未知或重复参数：complexity_profile');
  if(values.length===1&&!values[0])fail('complexity_profile不能为空'+punctuation);
  const id=values[0]||defaultProfile(meta),profile=profiles(meta).find(p=>p.id===id);
  if(!profile)fail('未知或未提供的复杂度样本方案'+punctuation);return profile;
}
function countryInfo(id){return {id,display_name:id==='enriched'?'国家补全版':'原始版',version:id==='enriched'?'country-enriched-v3-r3':'snapshot-20260626-original'};}

function defaultMethod(meta) {
  return (meta.methods||[]).some(m=>(typeof m==='string'?m:m.id)===xiaoNiMethod)?xiaoNiMethod:(meta.eci_default_method||'spectral-v1');
}
function presentMethod(method) { return method?.id===xiaoNiMethod?{...method,display_name:'小倪方法论'}:method; }
function presentMetadata(meta,catalog={}) {
  const available=catalog.complexity_profiles||meta.complexity_profiles||[baselineProfile];
  return {...meta,methods:meta.methods?.map(presentMethod),eci_default_method:defaultMethod(meta),complexity_profiles:available,default_complexity_profile:catalog.default_complexity_profile||meta.default_complexity_profile||'baseline-v1',profile_model_downloads:catalog.profile_model_downloads||meta.profile_model_downloads||[]};
}

class InputError extends Error {}
function fail(message) { throw new InputError(message); }
function apiError(status, code, error) { return json({code,error},status); }
function json(value,status=200) { return new Response(JSON.stringify(value)+'\n',{status,headers:{'Content-Type':MIME,'Cache-Control':'no-store'}}); }
function secure(response,corpus,head=false,countryVersion,complexityProfile) {
  const headers=new Headers(response.headers);
  headers.set('X-Content-Type-Options','nosniff');headers.set('Referrer-Policy','no-referrer');headers.set('Content-Security-Policy',CSP);
  if(corpus)headers.set('X-OpenAlex-Corpus',corpus);
  if(countryVersion)headers.set('X-OpenAlex-Country-Version',countryVersion);
  if(complexityProfile)headers.set('X-OpenAlex-Complexity-Profile',complexityProfile);
  return new Response(head?null:response.body,{status:response.status,statusText:response.statusText,headers});
}

async function readAsset(env,origin,path) {
  // Every path is produced by the signed-off static control manifest or by
  // validated ordinal indices. Request input is never a filesystem path.
  const key=origin+path;
  // Keep one immutable site control independent of the bounded model cache.
  // A 48-shard RCA request must not evict and reparse its 4MB control each time.
  const storage=path==='/data/site.json'?siteCache:cache,limit=storage===siteCache?1:MAX_CACHE;
  function touch(){const value=storage.get(key);storage.delete(key);storage.set(key,value);return value;}
  if(storage.has(key))return touch();
  // Never cache an in-flight fetch/stream promise. Workerd binds I/O objects
  // to their originating request, so sharing pending work can hang/cancel
  // otherwise valid concurrent requests. Only parsed plain data is reusable.
  const response=await env.ASSETS.fetch(new URL(path,origin));
  if(!response.ok)throw new Error(`static lookup unavailable (${response.status})`);
  let bytes=new Uint8Array(await response.arrayBuffer());
  if(storage.has(key))return touch();
  if(bytes[0]===0x1f&&bytes[1]===0x8b){
    bytes=new Uint8Array(await new Response(new Blob([bytes]).stream().pipeThrough(new DecompressionStream('gzip'))).arrayBuffer());
    if(storage.has(key))return touch();
  }
  let value;
  if(bytes[0]===79&&bytes[1]===65&&bytes[2]===81&&bytes[3]===49){const view=new DataView(bytes.buffer,bytes.byteOffset,bytes.byteLength),start=view.getUint16(4,true),years=view.getUint16(6,true),countries=view.getUint16(8,true);if(bytes.length!==16+(countries+1)*years*16)throw new Error('Invalid static matrix');value={view,start,years,countries};}
  else value=JSON.parse(new TextDecoder().decode(bytes));
  storage.set(key,value);while(storage.size>limit)storage.delete(storage.keys().next().value);
  return value;
}

function dictionary(meta){
  const countries=new Map(meta.countries.map((c,i)=>[c.id,{...c,ordinal:i}]));
  const layers=new Map(meta.layers.map((l,i)=>[`${l.scheme}/${l.level}`,{...l,ordinal:i,labelsByID:new Map(l.labels.map((d,j)=>[d.id,{...d,ordinal:j}]))}]));
  return {countries,layers};
}

export function parseQuery(params,meta){
  for(const key of new Set(params.keys()))if(!allowed.has(key)||params.getAll(key).length!==1)fail(`未知或重复参数：${key}`);
  const q={corpus:meta.corpus,scenario:params.get('scenario')||meta.eci_scenario_id,scheme:params.get('scheme')||'',level:params.get('level')||'',discipline:params.get('discipline')||'',disciplines:null,countries:null,metric:params.get('metric')||'publications',basis:params.get('basis')||'publications',start:Math.max(2000,meta.year_start),end:Math.min(2025,meta.year_end),method:params.get('method')||defaultMethod(meta)};
  q.complexity_profile=selectedProfile(params,meta).id;
  const methods=meta.methods||[{id:'spectral-v1'}];
  if(!methods.some(m=>(typeof m==='string'?m:m.id)===q.method))fail('未知或未提供的知识复杂度方法');
  const {countries,layers}=dictionary(meta);
  if(!meta.scenarios.some(s=>s.id===q.scenario))fail('未知统计场景');
  const layer=layers.get(`${q.scheme}/${q.level}`);
  if(!layer)fail('无效分类层级；Topics使用field/subfield，Concepts使用0/1');
  if(!['publications','citations'].includes(q.basis))fail('basis只能是publications或citations');
  if(!['publications','citations','share','rca','eci','pci'].includes(q.metric))fail('未知指标');
  if(params.has('disciplines')){
    if(params.has('discipline'))fail('discipline与disciplines不能同时提供');
    q.discipline=params.get('disciplines');if(!q.discipline)fail('disciplines不能为空');
  }else if(q.discipline.includes(','))fail('单学科参数discipline不能包含多个学科，请使用disciplines');
  if(q.discipline){
    const selected=[];for(const id of q.discipline.split(',')){if(!layer.labelsByID.has(id))fail(`学科参数不属于当前层级：${id}`);if(!selected.includes(id))selected.push(id);}q.disciplines=selected;
  }
  if(!q.disciplines?.length&&q.metric!=='eci')fail('请选择本层级中有效的学科');
  const max=meta.max_series||48;
  if((q.disciplines?.length||0)>max)fail(`最多选择${max}个学科`);
  q.discipline=q.disciplines?.[0]||'';
  if(q.metric!=='pci'){
    const selected=[];for(const id of (params.get('countries')||'').split(',')){if(!id)continue;if(!countries.has(id))fail(`未知国家／地区代码：${id}`);if(!selected.includes(id))selected.push(id);}
    const maxCountries=q.metric==='eci'?countries.size:12;
    if(!selected.length||selected.length>maxCountries)fail(`请选择1到${maxCountries}个国家／地区`);q.countries=selected;
  }
  const curves=q.metric==='eci'?q.countries.length:q.metric==='pci'?q.disciplines.length:q.countries.length*q.disciplines.length;
  if(q.metric!=='eci'&&curves>max)fail(`当前选择产生${curves}条曲线，最多${max}条；请减少国家或学科`);
  for(const key of ['start','end']){const raw=params.get(key);if(raw){if(!/^[+-]?\d+$/.test(raw)||!Number.isSafeInteger(Number(raw)))fail(`无效年份：${key}`);q[key]=Number(raw);}}
  if(q.start<Math.max(2000,meta.year_start)||q.end>meta.year_end||q.end<q.start||q.end-q.start>200)fail(`年份必须位于${Math.max(2000,meta.year_start)}—${meta.year_end}`);
  return q;
}

function modelKey(q){return [q.method,q.scenario,q.scheme,q.level,q.basis].join('/');}
function templateKey(q){return [q.method,q.scenario,q.scheme,q.level,q.metric,q.basis].join('/');}
function matrixValue(shard,country,year,basis){const offset=16+shard.years*16+(country*shard.years+year)*16+basis*8;return shard.view.getFloat64(offset,true);}
function matrixTotal(shard,year,basis){return shard.view.getFloat64(16+year*16+basis*8,true);}

async function profileDataset(site,profileID,version,corpus,control,env,origin){
  const ref=site.complexity_profile_manifests?.[profileID];
  if(!ref?.path)throw new Error('missing completed complexity profile');
  const manifest=await readAsset(env,origin,ref.path),dataset=manifest.datasets?.[version]?.[corpus];
  if(manifest.profile?.id!==profileID||manifest.direction_policy!==profilePolicy||!dataset?.profile_run_id||dataset.source_run_id!==control.run_id||dataset.source_completion_sha256!==control.completion_sha256)throw new Error('complexity profile source identity mismatch');
  return dataset;
}

export async function seriesResponse(q,control,env,origin){
  const meta=control.meta,{countries,layers}=dictionary(meta),layer=layers.get(`${q.scheme}/${q.level}`);
  const template=control.templates[templateKey(q)]||control.templates[[q.scenario,q.scheme,q.level,q.metric,q.basis].join('/')];
  if(!template)throw new Error('missing response contract');
  const response={...template,method:presentMethod(template.method),definition:template.definition?.replace(/Java\s*反射法(?:[·・]修正版)?/g,'小倪方法论'),query:q,series:[]};
  const profile=profiles(meta).find(p=>p.id===q.complexity_profile)||baselineProfile,applies=q.metric==='eci'||q.metric==='pci';
  const baseline=profile.baseline||profile.id==='baseline-v1',source=control.run_id||response.method?.source_run_id||'';
  const profileRun=applies?(baseline?source:control.profile_dataset?.profile_run_id||''):'';
  response.complexity_profile=profile;response.profile_applies=applies;response.profile_run_id=profileRun;
  if(applies&&!baseline)response.definition=response.definition?.replace(' 知识复杂度方法：',' 样本筛选先改变国家与学科赋值矩阵，再重新计算 RCA 及 ECI/PCI；不按所选曲线重新排名。 知识复杂度方法：');
  response.model_context={profile_id:profile.id,profile_run_id:profileRun,source_run_id:source,source_completion_sha256:control.completion_sha256||'',algorithm:baseline?'baseline-v1':supportAlgorithm,direction_policy:baseline?baselinePolicy:profilePolicy,years:[]};
  const disciplines=q.metric==='eci'?['']:q.disciplines;
  const countryIDs=q.metric==='pci'?['GLOBAL']:q.countries;
  const partial=new Set(meta.partial_years);
  let model=null,margins=null;const shards=new Map();
  if(q.metric==='eci'||q.metric==='pci'){
    const path=control.models[modelKey(q)]||(!meta.methods?control.models[[q.scenario,q.scheme,q.level,q.basis].join('/')]:null);
    if(path)model=await readAsset(env,origin,path);
    if(!baseline&&model){
      const context=model.model_context;
      if(!context||context.profile_id!==profile.id||context.direction_policy!==profilePolicy||context.profile_run_id!==profileRun||context.source_run_id!==source||context.source_completion_sha256!==control.completion_sha256)throw new Error('model shard profile identity mismatch');
      response.model_context={...context,years:(context.years||[]).filter(y=>y.year>=q.start&&y.year<=q.end)};
    }
  }else{
    const scenario=meta.scenarios.findIndex(s=>s.id===q.scenario);
    const prefix=`${control.prefix}/counts/${scenario}/${layer.ordinal}`;
    await Promise.all(disciplines.map(async id=>{shards.set(id,await readAsset(env,origin,`${prefix}/${layer.labelsByID.get(id).ordinal}.bin.gz`));}));
    if(q.metric==='rca')margins=await readAsset(env,origin,`${prefix}/margins.bin.gz`);
  }
  for(const country of countryIDs)for(const discipline of disciplines){
    const countryName=country==='GLOBAL'?'全球学科 PCI':countries.get(country).display_name;
    const disciplineName=discipline?(layer.labelsByID.get(discipline)?.display_name||discipline):'';
    const multi=disciplines.length>1;
    const s={id:multi?`${country}::${discipline}`:country,name:multi?`${countryName} · ${disciplineName}`:countryName,country,country_name:countryName,discipline,discipline_name:disciplineName,points:[]};
    for(let year=q.start;year<=q.end;year++){
      const yi=year-meta.year_start,p={year,value:null,status:'ok',partial:partial.has(year),rank:null,rank_universe:0};
      if(q.metric==='eci'||q.metric==='pci'){
        const ordinal=q.metric==='eci'?countries.get(country).ordinal:layer.labelsByID.get(discipline).ordinal;
        const point=(q.metric==='eci'?model?.countries:model?.disciplines)?.[ordinal]?.[yi];
        if(point){
          p.value=point[0];p.status=point[1];p.rank=point[2];p.rank_universe=point[3];
          if(q.metric==='eci'&&!baseline&&point.length>=8){
            p.support={whole_publications:point[5],whole_citations:point[6],final_diversity:point[7]};
            const mask=point[4]||0;if(mask)p.filter_reasons=reasonNames.filter((_,i)=>mask&(1<<i));
          }
        }else p.status='model_not_available';
      }else{
        const shard=shards.get(discipline),ci=countries.get(country).ordinal;
        const basis=q.basis==='citations'?1:0;
        let value=matrixValue(shard,ci,yi,q.metric==='citations'?1:0);
        if(q.metric==='share'||q.metric==='rca'){
          value=matrixValue(shard,ci,yi,basis);const col=matrixTotal(shard,yi,basis);
          if(col<=0)p.status='no_denominator';
          else if(q.metric==='share')value/=col;
          else{const row=matrixValue(margins,ci,yi,basis),total=matrixTotal(margins,yi,basis);if(row<=0||total<=0)p.status='no_denominator';else value=(value/row)/(col/total);}
        }
        if(p.status==='ok')p.value=value;
      }
      s.points.push(p);
    }
    response.series.push(s);
  }
  return response;
}

export function goFloat(value){
  if(value==null)return '';
  if(value===0)return Object.is(value,-0)?'-0':'0';
  const abs=Math.abs(value);let text=abs<1e-4||abs>=1e6?value.toExponential():String(value);
  return text.replace(/e([+-])(\d)$/,(_,sign,exponent)=>`e${sign}0${exponent}`);
}
function csvCell(value){const s=String(value??'');return /[",\r\n]/.test(s)?`"${s.replaceAll('"','""')}"`:s;}
export function csvResponse(response,header){
  header ||= ['scenario','scheme','level','discipline','metric','basis','country','year','value','status','partial','corpus','rank','rank_universe','series_id','country_name','discipline_name'];
  if(!header.includes('country_version'))header=[...header,'country_version'];
  for(const key of ['complexity_profile','profile_applies','profile_run_id'])if(!header.includes(key))header=[...header,key];
  const lines=[header.join(',')];
  for(const s of response.series)for(const p of s.points){
    const cells={scenario:response.query.scenario,scheme:response.query.scheme,level:response.query.level,discipline:s.discipline,metric:response.query.metric,basis:response.query.basis,country:s.country,year:p.year,value:goFloat(p.value),status:p.status,partial:String(p.partial),corpus:response.query.corpus,rank:p.rank??'',rank_universe:p.rank_universe,series_id:s.id,country_name:s.country_name,discipline_name:s.discipline_name,method:response.query.method,country_iterations:response.method?.country_iterations||0,discipline_iterations:response.method?.discipline_iterations||0,source_run_id:response.method?.source_run_id||'',method_applies:String(Boolean(response.method_applies)),country_version:response.query.country_version||'original'};
    cells.complexity_profile=response.query.complexity_profile||'baseline-v1';cells.profile_applies=String(Boolean(response.profile_applies));cells.profile_run_id=response.profile_run_id||'';
    lines.push(header.map(key=>csvCell(cells[key])).join(','));
  }
  return new Response(lines.join('\n')+'\n',{headers:{'Content-Type':'text/csv; charset=utf-8','Content-Disposition':'attachment; filename="openalex-series.csv"','Cache-Control':'no-store'}});
}

export async function handle(request,env){
  const url=new URL(request.url),head=request.method==='HEAD';let corpus,countryVersion,complexityProfileID;
  const finish=response=>secure(response,corpus,head,countryVersion,complexityProfileID);
  try{
    if(!['GET','HEAD'].includes(request.method)){const r=apiError(405,'method_not_allowed','只支持 GET 或 HEAD。');r.headers.set('Allow','GET, HEAD');return secure(r,null,head);}
    if(!['/api/catalog','/api/meta','/api/series','/api/series.csv','/download'].includes(url.pathname))return secure(await env.ASSETS.fetch(request),null,head);
    const site=await readAsset(env,url.origin,'/data/site.json');
    if(site.version!=='openalex-static-query-v1')throw new Error('unsupported static export');
    if(url.pathname==='/api/catalog'){
      if([...url.searchParams].length)return secure(apiError(400,'invalid_parameter','catalog接口不接受参数。'),null,head);
      let catalog=site.country_versions?site.catalog:{...site.catalog,default_country_version:'original',country_versions:[countryInfo('original')]};
      catalog={...catalog,complexity_profiles:profiles(catalog),default_complexity_profile:defaultProfile(catalog)};
      // Project the public Go Catalog; private export hints never leak here.
      return finish(json({default_corpus:catalog.default_corpus,datasets:catalog.datasets,default_country_version:catalog.default_country_version,country_versions:catalog.country_versions,default_complexity_profile:catalog.default_complexity_profile,complexity_profiles:catalog.complexity_profiles}));
    }
    corpus=site.catalog.default_corpus;
    if(url.searchParams.has('corpus')){const values=url.searchParams.getAll('corpus');if(values.length!==1||!values[0])fail('corpus必须唯一且为core或all。');corpus=values[0];}
    if(!['core','all'].includes(corpus))fail('未知或未提供的语料版本。');
    countryVersion=site.catalog.default_country_version||(site.country_versions?.enriched?'enriched':'original');
    if(url.searchParams.has('country_version')){const values=url.searchParams.getAll('country_version');if(values.length!==1||!values[0])fail('country_version必须唯一且为original或enriched。');countryVersion=values[0];}
    if(!['original','enriched'].includes(countryVersion))fail('未知或未提供的国家数据版本。');
    const versionDatasets=site.country_versions?site.country_versions[countryVersion]:(countryVersion==='original'?site.datasets:null);
    if(!versionDatasets)fail('未知或未提供的国家数据版本。');
    const storedControl=versionDatasets[corpus];if(!storedControl)fail('未知或未提供的语料版本。');
    // Presentation defaults do not mutate the immutable exported manifest/models.
    const info=site.catalog.country_versions?.find(v=>v.id===countryVersion)||countryInfo(countryVersion);
    let control={...storedControl,meta:{...presentMetadata(storedControl.meta,site.catalog),country_version:countryVersion,country_version_info:info}};
    complexityProfileID=defaultProfile(control.meta);
    const params=new URLSearchParams(url.searchParams);params.delete('corpus');params.delete('country_version');
    if(url.pathname==='/api/meta'){
      if([...params].length)fail('metadata接口不接受参数。');return finish(json({...control.meta,max_eci_countries:control.meta.countries.length}));
    }
    if(url.pathname==='/download'){
      for(const key of new Set(params.keys()))if(!['file','method','complexity_profile'].includes(key)||params.getAll(key).length!==1)fail('只能下载已列出的统计产物。');
      if(params.getAll('file').length!==1)fail('必须提供唯一file参数。');
      const method=params.get('method')||defaultMethod(control.meta);
      if(!(control.meta.methods||[{id:'spectral-v1'}]).some(m=>(typeof m==='string'?m:m.id)===method))fail('未知或未提供的知识复杂度方法。');
      const profile=selectedProfile(params,control.meta,'。');complexityProfileID=profile.id;
      const name=params.get('file'),profileModelFile=['eci.json','country_eci.parquet','discipline_pci.parquet','model_diagnostics.json'].includes(name)||name.startsWith('method-')||name.startsWith('profile-');
      let methodArtifact=own(control.method_downloads?.[method],name),artifact=methodArtifact||own(control.downloads,name),profileBound=false,downloadProfileRunID='';
      if(!profile.baseline&&profile.id!=='baseline-v1'&&profileModelFile){
        const dataset=await profileDataset(site,profile.id,countryVersion,corpus,control,env,url.origin);
        methodArtifact=own(dataset.downloads?.[method],name);artifact=methodArtifact||own(dataset.downloads?.shared,name);profileBound=true;downloadProfileRunID=dataset.profile_run_id;
        if(!artifact)fail('该文件不是当前筛选模型的产物。');
      }else if(!methodArtifact&&!(control.meta.downloads||[]).includes(name))fail('只能下载已列出的统计产物。');
      if(!artifact)return finish(new Response('404 page not found\n',{status:404}));
      if(!artifact.path){const redirect=Response.redirect(artifact.url,302),headers=new Headers(redirect.headers);if(methodArtifact)headers.set('X-OpenAlex-Method',method);headers.set('X-OpenAlex-Profile-Applies',String(profileBound));return finish(new Response(null,{status:302,headers}));}
      const target=new URL(artifact.path,url.origin);const headers=new Headers();const range=request.headers.get('Range');if(range)headers.set('Range',range);
      if(artifact.encoding==='gzip')headers.delete('Range');
      const result=await env.ASSETS.fetch(new Request(target,{method:request.method,headers}));
      let filename=artifact.filename||name;if(methodArtifact&&!filename.startsWith(method))filename=`${method}-${filename}`;
      if(profileBound&&!filename.startsWith(profile.id))filename=`${profile.id}-${methodArtifact?'':method+'-'}${filename}`;
      const outgoing=new Headers(result.headers);outgoing.set('Content-Disposition',`attachment; filename="${filename}"`);outgoing.set('X-Artifact-SHA256',artifact.sha256);outgoing.set('X-OpenAlex-Profile-Applies',String(profileBound));if(methodArtifact)outgoing.set('X-OpenAlex-Method',method);
      if(profileBound){outgoing.set('Content-Disposition',`attachment; filename="${profile.id}-${method}-${name}"`);outgoing.set('X-OpenAlex-Profile-Run-ID',downloadProfileRunID);outgoing.set('X-OpenAlex-Method',method);outgoing.set('Cache-Control','no-store');if(name.endsWith('.json'))outgoing.set('Content-Type','application/json; charset=utf-8');else outgoing.set('Content-Type','application/octet-stream');}
      let body=result.body;
      if(artifact.encoding==='gzip'&&result.ok){outgoing.delete('Content-Encoding');outgoing.delete('Accept-Ranges');outgoing.set('Content-Type','application/json; charset=utf-8');outgoing.set('Content-Length',String(artifact.bytes));outgoing.set('ETag',`"${artifact.sha256}"`);if(!head)body=body.pipeThrough(new DecompressionStream('gzip'));}
      // ASSETS implementations can report zero for a HEAD response's empty
      // body. The verified artifact describes the original entity length;
      // preserve a 206 response's independently supplied range length.
      if(head&&result.status===200)outgoing.set('Content-Length',String(artifact.bytes));
      return finish(new Response(body,{status:result.status,headers:outgoing}));
    }
    const q={...parseQuery(params,control.meta),country_version:countryVersion};complexityProfileID=q.complexity_profile;
    if((q.metric==='eci'||q.metric==='pci')&&q.complexity_profile!=='baseline-v1'){
      const dataset=await profileDataset(site,q.complexity_profile,countryVersion,corpus,control,env,url.origin);
      control={...control,models:dataset.models,profile_dataset:dataset};
    }
    const response=await seriesResponse(q,control,env,url.origin);
    return finish(url.pathname==='/api/series.csv'?csvResponse(response,control.csv_header):json(response));
  }catch(error){
    return finish(error instanceof InputError?apiError(400,'invalid_parameter',error.message):apiError(500,'static_data_unavailable','静态查询数据暂不可用，请检查本次部署的数据清单。'));
  }
}

export default {fetch:handle};
