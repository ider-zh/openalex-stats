// Cloudflare Pages advanced-mode adapter. Scientific values and global ranks
// are exported by Go; this module only validates filters and reads lookup data.
const MIME = 'application/json; charset=utf-8';
const CSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'";
const allowed = new Set(['scenario','scheme','level','discipline','disciplines','countries','metric','basis','start','end','method']);
const cache = new Map();
const MAX_CACHE = 32;

class InputError extends Error {}
function fail(message) { throw new InputError(message); }
function apiError(status, code, error) { return json({code,error},status); }
function json(value,status=200) { return new Response(JSON.stringify(value)+'\n',{status,headers:{'Content-Type':MIME,'Cache-Control':'no-store'}}); }
function secure(response,corpus,head=false) {
  const headers=new Headers(response.headers);
  headers.set('X-Content-Type-Options','nosniff');headers.set('Referrer-Policy','no-referrer');headers.set('Content-Security-Policy',CSP);
  if(corpus)headers.set('X-OpenAlex-Corpus',corpus);
  return new Response(head?null:response.body,{status:response.status,statusText:response.statusText,headers});
}

async function readAsset(env,origin,path) {
  // Every path is produced by the signed-off static control manifest or by
  // validated ordinal indices. Request input is never a filesystem path.
  const key=origin+path;
  if(cache.has(key)){const value=cache.get(key);cache.delete(key);cache.set(key,value);return value;}
  const pending=(async()=>{
    const response=await env.ASSETS.fetch(new URL(path,origin));
    if(!response.ok)throw new Error(`static lookup unavailable (${response.status})`);
    let bytes=new Uint8Array(await response.arrayBuffer());
    if(bytes[0]===0x1f&&bytes[1]===0x8b){
      bytes=new Uint8Array(await new Response(new Blob([bytes]).stream().pipeThrough(new DecompressionStream('gzip'))).arrayBuffer());
    }
    if(bytes[0]===79&&bytes[1]===65&&bytes[2]===81&&bytes[3]===49){const view=new DataView(bytes.buffer,bytes.byteOffset,bytes.byteLength),start=view.getUint16(4,true),years=view.getUint16(6,true),countries=view.getUint16(8,true);if(bytes.length!==16+(countries+1)*years*16)throw new Error('Invalid static matrix');return {view,start,years,countries};}
    return JSON.parse(new TextDecoder().decode(bytes));
  })();
  cache.set(key,pending);while(cache.size>MAX_CACHE)cache.delete(cache.keys().next().value);
  try{return await pending;}catch(error){cache.delete(key);throw error;}
}

function dictionary(meta){
  const countries=new Map(meta.countries.map((c,i)=>[c.id,{...c,ordinal:i}]));
  const layers=new Map(meta.layers.map((l,i)=>[`${l.scheme}/${l.level}`,{...l,ordinal:i,labelsByID:new Map(l.labels.map((d,j)=>[d.id,{...d,ordinal:j}]))}]));
  return {countries,layers};
}

export function parseQuery(params,meta){
  for(const key of new Set(params.keys()))if(!allowed.has(key)||params.getAll(key).length!==1)fail(`未知或重复参数：${key}`);
  const q={corpus:meta.corpus,scenario:params.get('scenario')||meta.eci_scenario_id,scheme:params.get('scheme')||'',level:params.get('level')||'',discipline:params.get('discipline')||'',disciplines:null,countries:null,metric:params.get('metric')||'publications',basis:params.get('basis')||'publications',start:Math.max(2000,meta.year_start),end:Math.min(2025,meta.year_end),method:params.get('method')||meta.eci_default_method||'spectral-v1'};
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
    if(!selected.length||selected.length>12)fail('请选择1到12个国家／地区');q.countries=selected;
  }
  const curves=q.metric==='eci'?q.countries.length:q.metric==='pci'?q.disciplines.length:q.countries.length*q.disciplines.length;
  if(curves>max)fail(`当前选择产生${curves}条曲线，最多${max}条；请减少国家或学科`);
  for(const key of ['start','end']){const raw=params.get(key);if(raw){if(!/^[+-]?\d+$/.test(raw)||!Number.isSafeInteger(Number(raw)))fail(`无效年份：${key}`);q[key]=Number(raw);}}
  if(q.start<Math.max(2000,meta.year_start)||q.end>meta.year_end||q.end<q.start||q.end-q.start>200)fail(`年份必须位于${Math.max(2000,meta.year_start)}—${meta.year_end}`);
  return q;
}

function modelKey(q){return [q.method,q.scenario,q.scheme,q.level,q.basis].join('/');}
function templateKey(q){return [q.method,q.scenario,q.scheme,q.level,q.metric,q.basis].join('/');}
function matrixValue(shard,country,year,basis){const offset=16+shard.years*16+(country*shard.years+year)*16+basis*8;return shard.view.getFloat64(offset,true);}
function matrixTotal(shard,year,basis){return shard.view.getFloat64(16+year*16+basis*8,true);}

export async function seriesResponse(q,control,env,origin){
  const meta=control.meta,{countries,layers}=dictionary(meta),layer=layers.get(`${q.scheme}/${q.level}`);
  const template=control.templates[templateKey(q)]||control.templates[[q.scenario,q.scheme,q.level,q.metric,q.basis].join('/')];
  if(!template)throw new Error('missing response contract');
  const response={...template,query:q,series:[]};
  const disciplines=q.metric==='eci'?['']:q.disciplines;
  const countryIDs=q.metric==='pci'?['GLOBAL']:q.countries;
  const partial=new Set(meta.partial_years);
  let model=null,margins=null;const shards=new Map();
  if(q.metric==='eci'||q.metric==='pci'){
    const path=control.models[modelKey(q)]||(!meta.methods?control.models[[q.scenario,q.scheme,q.level,q.basis].join('/')]:null);
    if(path)model=await readAsset(env,origin,path);
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
        if(point){p.value=point[0];p.status=point[1];p.rank=point[2];p.rank_universe=point[3];}else p.status='model_not_available';
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
  const lines=[header.join(',')];
  for(const s of response.series)for(const p of s.points){
    const cells={scenario:response.query.scenario,scheme:response.query.scheme,level:response.query.level,discipline:s.discipline,metric:response.query.metric,basis:response.query.basis,country:s.country,year:p.year,value:goFloat(p.value),status:p.status,partial:String(p.partial),corpus:response.query.corpus,rank:p.rank??'',rank_universe:p.rank_universe,series_id:s.id,country_name:s.country_name,discipline_name:s.discipline_name,method:response.query.method,country_iterations:response.method?.country_iterations||0,discipline_iterations:response.method?.discipline_iterations||0,source_run_id:response.method?.source_run_id||'',method_applies:String(Boolean(response.method_applies))};
    lines.push(header.map(key=>csvCell(cells[key])).join(','));
  }
  return new Response(lines.join('\n')+'\n',{headers:{'Content-Type':'text/csv; charset=utf-8','Content-Disposition':'attachment; filename="openalex-series.csv"','Cache-Control':'no-store'}});
}

export async function handle(request,env){
  const url=new URL(request.url),head=request.method==='HEAD';let corpus;
  try{
    if(!['GET','HEAD'].includes(request.method)){const r=apiError(405,'method_not_allowed','只支持 GET 或 HEAD。');r.headers.set('Allow','GET, HEAD');return secure(r,null,head);}
    if(!['/api/catalog','/api/meta','/api/series','/api/series.csv','/download'].includes(url.pathname))return secure(await env.ASSETS.fetch(request),null,head);
    const site=await readAsset(env,url.origin,'/data/site.json');
    if(site.version!=='openalex-static-query-v1')throw new Error('unsupported static export');
    if(url.pathname==='/api/catalog'){
      if([...url.searchParams].length)return secure(apiError(400,'invalid_parameter','catalog接口不接受参数。'),null,head);
      return secure(json(site.catalog),null,head);
    }
    corpus=site.catalog.default_corpus;
    if(url.searchParams.has('corpus')){const values=url.searchParams.getAll('corpus');if(values.length!==1||!values[0])fail('corpus必须唯一且为core或all。');corpus=values[0];}
    const control=site.datasets[corpus];if(!control)fail('未知或未提供的语料版本。');
    const params=new URLSearchParams(url.searchParams);params.delete('corpus');
    if(url.pathname==='/api/meta'){
      if([...params].length)fail('metadata接口不接受参数。');return secure(json(control.meta),corpus,head);
    }
    if(url.pathname==='/download'){
      for(const key of new Set(params.keys()))if(!['file','method'].includes(key)||params.getAll(key).length!==1)fail('只能下载已列出的统计产物。');
      if(params.getAll('file').length!==1)fail('必须提供唯一file参数。');
      const method=params.get('method')||control.meta.eci_default_method||'spectral-v1';
      if(!(control.meta.methods||[{id:'spectral-v1'}]).some(m=>(typeof m==='string'?m:m.id)===method))fail('未知或未提供的知识复杂度方法。');
      const name=params.get('file'),methodArtifact=control.method_downloads?.[method]?.[name];
      if(!methodArtifact&&!(control.meta.downloads||[]).includes(name))fail('只能下载已列出的统计产物。');
      const artifact=methodArtifact||control.downloads[name];if(!artifact)return secure(new Response('404 page not found\n',{status:404}),corpus,head);
      if(!artifact.path){const redirect=Response.redirect(artifact.url,302),headers=new Headers(redirect.headers);if(methodArtifact)headers.set('X-OpenAlex-Method',method);return secure(new Response(null,{status:302,headers}),corpus,head);}
      const target=new URL(artifact.path,url.origin);const headers=new Headers();const range=request.headers.get('Range');if(range)headers.set('Range',range);
      const result=await env.ASSETS.fetch(new Request(target,{method:request.method,headers}));
      let filename=artifact.filename||name;if(methodArtifact&&!filename.startsWith(method))filename=`${method}-${filename}`;
      const outgoing=new Headers(result.headers);outgoing.set('Content-Disposition',`attachment; filename="${filename}"`);outgoing.set('X-Artifact-SHA256',artifact.sha256);if(methodArtifact)outgoing.set('X-OpenAlex-Method',method);
      return secure(new Response(result.body,{status:result.status,headers:outgoing}),corpus,head);
    }
    const q=parseQuery(params,control.meta),response=await seriesResponse(q,control,env,url.origin);
    return secure(url.pathname==='/api/series.csv'?csvResponse(response,control.csv_header):json(response),corpus,head);
  }catch(error){
    return secure(error instanceof InputError?apiError(400,'invalid_parameter',error.message):apiError(500,'static_data_unavailable','静态查询数据暂不可用，请检查本次部署的数据清单。'),corpus,head);
  }
}

export default {fetch:handle};
