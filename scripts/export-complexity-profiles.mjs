// Encode completed Go profile outputs as static lookup shards. Never solves,
// chooses an axis, filters a matrix, or calculates replacement ranks in Node.
import fs from 'node:fs/promises';
import path from 'node:path';
import {createHash} from 'node:crypto';
import {gzipSync} from 'node:zlib';
import {pathToFileURL} from 'node:url';

const STATIC='openalex-static-query-v1',BUNDLE='complexity-filter-bundle-v1';
const ALGORITHM='country-scale-cell-zero-monotone-diversity-v1';
const DIRECTION_POLICY='baseline-pooled-common-year-sign-v1';
const methods=['spectral-v1','java-reflections-v1'];
const digest=b=>createHash('sha256').update(b).digest('hex');
const idPattern=/^[a-zA-Z0-9_.-]+$/;
function requireValue(good,message){if(!good)throw new Error(message);}
function relativeFile(root,relative){
  requireValue(typeof relative==='string'&&relative!==''&&!relative.includes('\\')&&path.posix.normalize(relative)===relative&&!path.isAbsolute(relative),'Invalid artifact relative path');
  const p=path.resolve(root,relative);requireValue(p.startsWith(root+path.sep),'Artifact path escapes root');return p;
}
async function checkedFile(root,a){
  const p=relativeFile(root,a.path),actual=await fs.realpath(p);requireValue(actual.startsWith(root+path.sep),'Artifact symlink escapes root');
  const b=await fs.readFile(actual);requireValue(b.length===a.bytes&&digest(b)===a.sha256,`Artifact checksum mismatch: ${a.path}`);return b;
}
function profileInfo(profile){
  requireValue(idPattern.test(profile.id),'Invalid profile ID');
  for(const k of ['country_min_publications','country_min_citation_events','cell_min_support','min_diversity'])requireValue(Number.isFinite(profile[k])&&profile[k]>=0,`Invalid threshold ${k}`);
  const baseline=profile.id==='baseline-v1';return {...profile,display_name:baseline?'不筛选 · 原模型':profile.display_name,engine:baseline?'baseline-v1':'support-filter-v1',baseline,direction_policy:baseline?'published-baseline-v1':DIRECTION_POLICY};
}
const coordinate=m=>[m.year,m.scheme,String(m.level),m.metric].join('/');
function indexed(rows,label){const map=new Map();for(const row of rows){const key=coordinate(row);requireValue(!map.has(key),`Duplicate ${label} ${key}`);map.set(key,row);}return map;}
function maskOf(d){let mask=0;for(const reason of d.reasons||[]){if(reason==='country_min_publications')mask|=1;else if(reason==='country_min_citation_events')mask|=2;else if(reason==='min_diversity')mask|=8;else if(reason==='zero_matrix_support')mask|=d.original_matrix_mass>0?4:16;else throw new Error('Unknown country exclusion reason '+reason);}return mask;}
function filteredStatus(mask){for(const [bit,name] of [[1,'country_publications'],[2,'country_citations'],[4,'cell_support'],[8,'min_diversity']])if(mask&bit)return 'filtered_'+name;return '';}
function sameLabels(labels,expected,what){const wanted=new Set(expected);requireValue(labels.length===expected.length&&new Set(labels).size===labels.length&&labels.every(id=>wanted.has(id)),`Model ${what} label universe mismatch`);}

export function encodeModelGroups({models,ranks,diagnostics,meta,profile,profileRunID,sourceRunID,sourceCompletionSHA256,scenario}){
  const ranksByKey=indexed(ranks,'rank'),diagByKey=indexed(diagnostics,'diagnostic'),modelsByKey=indexed(models,'model'),groups=new Map();
  const years=meta.year_end-meta.year_start+1,expected=years*meta.layers.length*2;
  requireValue(modelsByKey.size===expected&&ranksByKey.size===expected&&diagByKey.size===expected,'Incomplete model/rank/diagnostic coverage');
  const countryIDs=meta.countries.map(c=>c.id),countryOrdinals=new Map(countryIDs.map((id,i)=>[id,i]));
  for(const layer of meta.layers)for(const basis of ['publications','citations']){
    const key=[layer.scheme,String(layer.level),basis].join('/'),context={profile_id:profile.id,profile_run_id:profileRunID,source_run_id:sourceRunID,source_completion_sha256:sourceCompletionSHA256,algorithm:ALGORITHM,direction_policy:DIRECTION_POLICY,years:[]};
    const shard={countries:countryIDs.map(()=>Array.from({length:years},()=>[null,'model_not_available',null,0,0,0,0,0])),disciplines:layer.labels.map(()=>Array.from({length:years},()=>[null,'model_not_available',null,0])),model_context:context};
    groups.set(key,{scheme:layer.scheme,level:String(layer.level),basis,scenario,shard});
  }
  for(const [key,m] of modelsByKey){
    const group=groups.get([m.scheme,String(m.level),m.metric].join('/'));requireValue(group&&m.year>=meta.year_start&&m.year<=meta.year_end,'Unexpected model coordinates');
    const r=m.result,nativeRanks=ranksByKey.get(key),diagnostic=diagByKey.get(key)?.diagnostics;
    requireValue(nativeRanks&&diagnostic&&diagnostic.algorithm===ALGORITHM&&diagnostic.profile.id===profile.id,'Missing matching rank/diagnostic');
    for(const field of ['country_min_publications','country_min_citation_events','cell_min_support','min_diversity'])requireValue(diagnostic.profile[field]===profile[field],'Diagnostic profile configuration mismatch');
    const layer=meta.layers.find(l=>l.scheme===m.scheme&&String(l.level)===String(m.level)),disciplineIDs=layer.labels.map(d=>d.id),disciplineOrdinals=new Map(disciplineIDs.map((id,i)=>[id,i]));
    sameLabels(r.countries,countryIDs,'country');sameLabels(r.disciplines,disciplineIDs,'discipline');
    const decisions=new Map((diagnostic.countries||[]).map(d=>[d.country,d]));requireValue(decisions.size===countryIDs.length&&decisions.size===diagnostic.countries.length,'Incomplete or duplicate country decisions');
    const yearIndex=m.year-meta.year_start,countryActive=new Map((r.active_countries||[]).map((i,p)=>[i,p])),disciplineActive=new Map((r.active_disciplines||[]).map((i,p)=>[i,p]));
    for(const [active,labels,scores,ranking,universe] of [[r.active_countries||[],r.countries,r.eci||[],nativeRanks.country_ranks||[],nativeRanks.country_universe],[r.active_disciplines||[],r.disciplines,r.pci||[],nativeRanks.discipline_ranks||[],nativeRanks.discipline_universe]]){
      requireValue(new Set(active).size===active.length&&active.every(i=>Number.isInteger(i)&&i>=0&&i<labels.length),'Invalid active label indices');
      if(r.status==='ok')requireValue(scores.length===active.length&&ranking.length===active.length&&universe===active.length&&scores.every(Number.isFinite)&&ranking.every(v=>Number.isInteger(v)&&v>=1&&v<=universe),'Native model/rank cardinality mismatch');
      else requireValue(ranking.length===0&&universe===0,'Invalid model must not export valid ranks');
    }
    for(let i=0;i<r.countries.length;i++){
      const id=r.countries[i],decision=decisions.get(id);requireValue(decision,'Missing country decision');
      for(const field of ['whole_publications','whole_citations','final_diversity'])requireValue(Number.isSafeInteger(decision[field])&&decision[field]>=0,'Invalid country support value');
      const mask=maskOf(decision),point=[null,r.status==='ok'?'inactive':r.status,null,r.status==='ok'?nativeRanks.country_universe:0,mask,decision.whole_publications,decision.whole_citations,decision.final_diversity];
      const active=countryActive.get(i);if(r.status==='ok'&&active!==undefined){point[0]=r.eci[active];point[1]=r.unstable?'unstable':'ok';point[2]=nativeRanks.country_ranks[active];}
      const excluded=filteredStatus(mask);if(excluded){point[0]=null;point[1]=excluded;point[2]=null;}
      group.shard.countries[countryOrdinals.get(id)][yearIndex]=point;
    }
    for(let i=0;i<r.disciplines.length;i++){
      const point=[null,r.status==='ok'?'inactive':r.status,null,r.status==='ok'?nativeRanks.discipline_universe:0],active=disciplineActive.get(i);
      if(r.status==='ok'&&active!==undefined){point[0]=r.pci[active];point[1]=r.unstable?'unstable':'ok';point[2]=nativeRanks.discipline_ranks[active];}
      group.shard.disciplines[disciplineOrdinals.get(r.disciplines[i])][yearIndex]=point;
    }
    group.shard.model_context.years.push({year:m.year,status:r.status,eligible_country_count:(r.active_countries||[]).length,eligible_discipline_count:(r.active_disciplines||[]).length,retained_mass_fraction:diagnostic.retained_mass_fraction,closure_passes:(diagnostic.rounds||[]).length,source_matrix_sha256:diagnostic.source_matrix_sha256,final_matrix_sha256:diagnostic.final_matrix_sha256,final_network_sha256:diagnostic.final_network_sha256});
  }
  for(const group of groups.values())group.shard.model_context.years.sort((a,b)=>a.year-b.year);
  return [...groups.values()];
}

async function loadBase(directory){
  const root=await fs.realpath(directory),raw=await fs.readFile(path.join(root,'export-manifest.json')),manifest=JSON.parse(raw);
  requireValue(manifest.version===STATIC&&Array.isArray(manifest.artifacts),'Unsupported base static manifest');
  const files=new Map();let total=0;
  for(const a of manifest.artifacts){requireValue(a.path.startsWith('data/')&&!files.has(a.path),'Invalid/duplicate base artifact');await checkedFile(root,a);files.set(a.path,a);total+=a.bytes;}
  requireValue(files.size===manifest.files&&total===manifest.bytes,'Base manifest totals mismatch');
  const site=JSON.parse(await fs.readFile(path.join(root,'data/site.json'),'utf8'));requireValue(site.country_versions?.original&&site.country_versions?.enriched,'Need complete two-country-version base export');
  requireValue(!site.complexity_profile_manifests,'Base already contains complexity profiles; use original immutable country export');
  return {root,manifest,site,files,sha256:digest(raw)};
}

export async function exportComplexityProfiles({base,bundles,output,onProgress=()=>{}}){
  requireValue(base&&Array.isArray(bundles)&&bundles.length===4&&output,'Requires --base, four --bundle directories and --output');
  const input=await loadBase(base),out=path.resolve(output),parent=await fs.realpath(path.dirname(out));
  const sources=[];let infos,defaultID;const seen=new Set();
  for(const directory of bundles){
    const root=await fs.realpath(directory),raw=await fs.readFile(path.join(root,'manifest.json')),manifest=JSON.parse(raw);
    requireValue(manifest.version===BUNDLE&&Array.isArray(manifest.profiles)&&manifest.direction_policy===DIRECTION_POLICY,'Unsupported Go profile bundle or direction policy');
    let route;
    for(const version of ['original','enriched']){const control=input.site.country_versions[version][manifest.corpus];if(control?.run_id===manifest.source_run_id&&control?.completion_sha256===manifest.source_completion_sha256)route=version;}
    requireValue(route,'Go bundle does not bind any selected country/corpus source');
    const scope=route+'/'+manifest.corpus;requireValue(!seen.has(scope),'Duplicate country/corpus bundle');seen.add(scope);
    const control=input.site.country_versions[route][manifest.corpus],list=manifest.profiles.map(p=>profileInfo(p.profile));
    if(!infos){infos=list;defaultID=manifest.default_profile_id;}else requireValue(JSON.stringify(infos)===JSON.stringify(list)&&defaultID===manifest.default_profile_id,'Profile catalogs/defaults differ between datasets');
    requireValue(list.some(p=>p.id==='baseline-v1')&&list.some(p=>p.id===defaultID),'Missing baseline/default profile');
    requireValue(manifest.year_start===control.meta.year_start&&manifest.year_end===control.meta.year_end&&manifest.scenario_id===control.meta.eci_scenario_id,'Go bundle year/scenario mismatch');
    sources.push({root,manifest,control,route,sha256:digest(raw)});
  }
  for(const source of [input,...sources])requireValue(parent!==source.root&&!parent.startsWith(source.root+path.sep)&&out!==source.root&&!out.startsWith(source.root+path.sep),'Output must be outside immutable inputs');
  await fs.mkdir(out,{recursive:false});
  const artifacts=new Map(input.files),profileManifests=new Map(infos.filter(p=>!p.baseline).map(p=>[p.id,{version:'openalex-static-complexity-profile-v1',direction_policy:DIRECTION_POLICY,profile:p,datasets:{original:{},enriched:{}}}]));
  async function write(relative,bytes){
    requireValue(relative.startsWith('data/'),'Invalid output asset path');requireValue(bytes.length<=25*1024*1024,'Static asset exceeds 25MiB');
    const a={path:relative,bytes:bytes.length,sha256:digest(bytes)},old=artifacts.get(relative);
    if(old){requireValue(old.bytes===a.bytes&&old.sha256===a.sha256,'Conflicting output artifact '+relative);return a;}
    const target=relativeFile(out,relative);await fs.mkdir(path.dirname(target),{recursive:true});await fs.writeFile(target,bytes);artifacts.set(relative,a);return a;
  }
  const copy=[...input.files.values()].filter(a=>a.path!=='data/site.json');let cursor=0;
  await Promise.all(Array.from({length:8},async()=>{while(cursor<copy.length){const a=copy[cursor++],target=relativeFile(out,a.path);await fs.mkdir(path.dirname(target),{recursive:true});await fs.copyFile(relativeFile(input.root,a.path),target);}}));
  for(const source of sources){
    for(const entry of source.manifest.profiles){
      const info=infos.find(p=>p.id===entry.profile.id);if(info.baseline)continue;
      requireValue(/^[a-f0-9]{64}$/.test(entry.profile_run_id),'Invalid profile run identity');
      const prefix=`data/complexity-profiles-v1/${info.id}/${source.route}-${source.manifest.corpus}-${source.manifest.source_run_id.slice(0,12)}-${entry.profile_run_id.slice(0,12)}`;
      const dataset={source_run_id:source.manifest.source_run_id,source_completion_sha256:source.manifest.source_completion_sha256,profile_run_id:entry.profile_run_id,models:{},downloads:{shared:{}}};
      const cache=new Map();async function get(a){if(!cache.has(a.path))cache.set(a.path,await checkedFile(source.root,a));else requireValue(cache.get(a.path).length===a.bytes&&digest(cache.get(a.path))===a.sha256,'Conflicting source artifact identity');return cache.get(a.path);}
      const diagnostics=JSON.parse(await get(entry.diagnostics));
      for(const method of methods){
        const models=JSON.parse(await get(entry.models[method])),ranks=JSON.parse(await get(entry.ranks[method]));
        const groups=encodeModelGroups({models,ranks,diagnostics,meta:source.control.meta,profile:info,profileRunID:entry.profile_run_id,sourceRunID:source.manifest.source_run_id,sourceCompletionSHA256:source.manifest.source_completion_sha256,scenario:source.manifest.scenario_id});
        for(const group of groups){
          const bytes=Buffer.from(JSON.stringify(group.shard)+'\n');requireValue(bytes.length<=2*1024*1024,'Expanded profile model exceeds planned 2MiB');
          const relative=`${prefix}/models/${method}/${group.scheme}-${group.level}-${group.basis}.json.gz`;await write(relative,gzipSync(bytes,{level:9,mtime:0}));
          dataset.models[[method,source.manifest.scenario_id,group.scheme,group.level,group.basis].join('/')]='/'+relative;
        }
      }
      const downloadGroups=entry.downloads.methods?{...entry.downloads.methods,shared:entry.downloads.shared||{}}:entry.downloads;
      for(const [method,downloads] of Object.entries(downloadGroups)){
        requireValue(method==='shared'||methods.includes(method),'Unexpected download method');dataset.downloads[method]={};
        for(const [name,a] of Object.entries(downloads)){
          requireValue(idPattern.test(name),'Invalid logical download filename');const raw=await get(a),compressed=name.endsWith('.json');
          const relative=`${prefix}/downloads/${method}/${path.posix.basename(a.path)}${compressed?'.gz':''}`;
          await write(relative,compressed?gzipSync(raw,{level:9,mtime:0}):raw);
          dataset.downloads[method][name]={filename:name,path:'/'+relative,bytes:a.bytes,sha256:a.sha256,...(compressed?{encoding:'gzip'}:{})};
        }
      }
      profileManifests.get(info.id).datasets[source.route][source.manifest.corpus]=dataset;
      onProgress({country_version:source.route,corpus:source.manifest.corpus,profile_id:info.id,model_shards:Object.keys(dataset.models).length});
    }
  }
  const manifestRefs={};
  for(const [id,manifest] of profileManifests){
    const raw=Buffer.from(JSON.stringify(manifest)+'\n');requireValue(raw.length<=512*1024,'Profile control exceeds planned 512KiB');
    const relative=`data/complexity-profiles-v1/${id}/manifest-${digest(raw).slice(0,16)}.json`,a=await write(relative,raw);manifestRefs[id]={path:'/'+a.path,bytes:a.bytes,sha256:a.sha256};
  }
  const sample=profileManifests.values().next().value,first=sample.datasets.original.core,profileDownloads=Object.keys({...first.downloads.shared,...first.downloads['spectral-v1']}).sort();
  const site=input.site;site.catalog={...site.catalog,complexity_profiles:infos,default_complexity_profile:defaultID,profile_model_downloads:profileDownloads};site.complexity_profile_manifests=manifestRefs;
  for(const controls of Object.values(site.country_versions))for(const control of Object.values(controls)){
    control.meta={...control.meta,complexity_profiles:infos,default_complexity_profile:defaultID,profile_model_downloads:profileDownloads};
    for(const k of ['complexity_profile','profile_applies','profile_run_id'])if(!control.csv_header.includes(k))control.csv_header.push(k);
  }
  site.datasets=site.country_versions[site.catalog.default_country_version];
  const siteBytes=Buffer.from(JSON.stringify(site)+'\n');requireValue(siteBytes.length<=25*1024*1024,'Site control exceeds Pages single asset limit');
  artifacts.delete('data/site.json');await write('data/site.json',siteBytes);
  const list=[...artifacts.values()].sort((a,b)=>a.path.localeCompare(b.path));requireValue(list.length+300<=20000,'Profile data plus conservative UI allowance exceeds Pages 20000-file budget');
  const manifest={...input.manifest,complexity_profiles:infos,default_complexity_profile:defaultID,profile_source_exports:sources.map(s=>({country_version:s.route,corpus:s.manifest.corpus,source_run_id:s.manifest.source_run_id,bundle_manifest_sha256:s.sha256,engine_binary_sha256:s.manifest.engine_binary_sha256})),base_country_export_sha256:input.sha256,artifacts:list,files:list.length,bytes:list.reduce((n,a)=>n+a.bytes,0)};
  await fs.writeFile(path.join(out,'export-manifest.json'),JSON.stringify(manifest)+'\n');
  return {output:out,files:manifest.files,bytes:manifest.bytes,profiles:infos.length,nonbaseline_profiles:profileManifests.size,default_complexity_profile:defaultID,site_control_bytes:siteBytes.length,new_artifacts:list.length-input.files.size,baseline_scientific_artifacts_changed:0};
}

if(process.argv[1]&&import.meta.url===pathToFileURL(path.resolve(process.argv[1])).href){
  const options={bundles:[]};
  for(let i=2;i<process.argv.length;i+=2){const key=process.argv[i],value=process.argv[i+1];requireValue(['--base','--bundle','--output'].includes(key)&&value,'Usage: --base DIR --bundle DIR (four times) --output NEW_DIR');if(key==='--bundle')options.bundles.push(value);else{requireValue(!options[key.slice(2)],'Duplicate option');options[key.slice(2)]=value;}}
  options.onProgress=value=>console.error(JSON.stringify(value));console.log(JSON.stringify(await exportComplexityProfiles(options),null,2));
}
