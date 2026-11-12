// Combine two complete Go exports without changing any scientific shard bytes.
import fs from 'node:fs/promises';
import path from 'node:path';
import {createHash} from 'node:crypto';
import {pathToFileURL} from 'node:url';

const VERSION='openalex-static-query-v1';
export const countryVersionInfo=[
  {id:'original',display_name:'原始版',version:'snapshot-20260626-original'},
  {id:'enriched',display_name:'国家补全版',version:'country-enriched-v3-r3'},
];
const hash=bytes=>createHash('sha256').update(bytes).digest('hex');
function child(root,relative){
  if(typeof relative!=='string'||!relative.startsWith('data/')||relative.includes('\\')||path.posix.normalize(relative)!==relative)throw new Error('Invalid artifact path');
  const target=path.resolve(root,relative);
  if(!target.startsWith(root+path.sep))throw new Error('Artifact escapes export');
  return target;
}
async function readExport(directory){
  const root=await fs.realpath(directory),manifestBytes=await fs.readFile(path.join(root,'export-manifest.json'));
  const manifest=JSON.parse(manifestBytes);
  if(manifest.version!==VERSION||!Array.isArray(manifest.artifacts))throw new Error('Unsupported export manifest');
  const seen=new Map();let bytes=0;
  for(const item of manifest.artifacts){
    const target=child(root,item.path),resolved=await fs.realpath(target);
    if(!resolved.startsWith(root+path.sep))throw new Error('Artifact symlink escapes export');
    if(seen.has(item.path))throw new Error(`Duplicate artifact: ${item.path}`);
    const content=await fs.readFile(target);
    if(content.length!==item.bytes||hash(content)!==item.sha256)throw new Error(`Source checksum mismatch: ${item.path}`);
    if(content.length>25*1024*1024)throw new Error(`Asset exceeds Pages limit: ${item.path}`);
    seen.set(item.path,item);bytes+=content.length;
  }
  if(manifest.files!==seen.size||manifest.bytes!==bytes)throw new Error('Source manifest totals mismatch');
  if(!seen.has('data/site.json'))throw new Error('Missing verified site control');
  const site=JSON.parse(await fs.readFile(path.join(root,'data/site.json'),'utf8'));
  if(site.version!==VERSION||!site.datasets?.core||site.country_versions)throw new Error('Input must be a complete single-country-version export');
  const corpora=Object.keys(site.datasets).sort();
  if(corpora.some(c=>!['core','all'].includes(c)))throw new Error('Unsupported corpus');
  for(const corpus of corpora){
    const control=site.datasets[corpus];
    if(control.meta?.corpus!==corpus||!control.run_id||!control.prefix?.startsWith('/data/'))throw new Error('Invalid corpus identity');
    if(!manifest.corpora?.some(c=>c.corpus===corpus&&c.run_id===control.run_id&&c.completion_sha256===control.completion_sha256))throw new Error('Dataset identity does not match export manifest');
  }
  return {root,manifest,site,manifest_sha256:hash(manifestBytes)};
}
function bindDatasets(datasets,info){
  return Object.fromEntries(Object.entries(datasets).map(([id,control])=>[id,{
    ...control,meta:{...control.meta,country_version:info.id,country_version_info:info},
    csv_header:control.csv_header?.includes('country_version')?control.csv_header:[...control.csv_header,'country_version'],
  }]));
}

export async function combineCountryExports({original,enriched,output}){
  if(!original||!enriched||!output)throw new Error('Requires --original, --enriched and --output');
  const old=await readExport(original),fresh=await readExport(enriched),out=path.resolve(output);
  for(const input of [old.root,fresh.root]){
    const relation=path.relative(input,out);
    if(relation===''||(!relation.startsWith('..'+path.sep)&&relation!=='..'&&!path.isAbsolute(relation)))throw new Error('Output must be outside both inputs');
  }
  if(JSON.stringify(Object.keys(old.site.datasets).sort())!==JSON.stringify(Object.keys(fresh.site.datasets).sort()))throw new Error('Both country versions must provide the same corpora');
  for(const corpus of Object.keys(old.site.datasets)){
    const a=old.site.datasets[corpus],b=fresh.site.datasets[corpus];
    if(a.run_id===b.run_id||a.prefix===b.prefix)throw new Error('Country versions must have distinct run identities and prefixes');
    for(const field of ['selected_works','snapshot_date','year_start','year_end'])if(a.meta[field]!==b.meta[field])throw new Error(`Country versions changed immutable corpus field ${field}`);
  }
  // Preflight every collision before creating output. Scientific artifacts are copied verbatim.
  const artifacts=new Map(),sources=new Map();
  for(const input of [old,fresh])for(const item of input.manifest.artifacts){
    if(item.path==='data/site.json')continue;
    const previous=artifacts.get(item.path);
    if(previous&&(previous.bytes!==item.bytes||previous.sha256!==item.sha256))throw new Error(`Conflicting export path: ${item.path}`);
    artifacts.set(item.path,item);sources.set(item.path,input.root);
  }
  const parents=await fs.realpath(path.dirname(out));
  for(const input of [old.root,fresh.root])if(parents===input||parents.startsWith(input+path.sep))throw new Error('Resolved output parent is inside input');
  await fs.mkdir(out,{recursive:false});
  for(const item of artifacts.values()){
    const target=child(out,item.path);await fs.mkdir(path.dirname(target),{recursive:true});
    await fs.copyFile(child(sources.get(item.path),item.path),target);
  }
  const versions={original:bindDatasets(old.site.datasets,countryVersionInfo[0]),enriched:bindDatasets(fresh.site.datasets,countryVersionInfo[1])};
  const site={version:VERSION,catalog:{...fresh.site.catalog,country_versions:countryVersionInfo,default_country_version:'enriched'},datasets:versions.enriched,country_versions:versions};
  const siteBytes=Buffer.from(JSON.stringify(site)+'\n');
  await fs.writeFile(path.join(out,'data/site.json'),siteBytes);
  artifacts.set('data/site.json',{path:'data/site.json',bytes:siteBytes.length,sha256:hash(siteBytes)});
  const list=[...artifacts.values()].sort((a,b)=>a.path.localeCompare(b.path));
  if(list.length+300>20000||siteBytes.length>25*1024*1024)throw new Error('Combined export exceeds conservative Pages asset limits');
  const manifest={version:VERSION,default_country_version:'enriched',country_versions:countryVersionInfo,
    source_exports:[{country_version:'original',manifest_sha256:old.manifest_sha256},{country_version:'enriched',manifest_sha256:fresh.manifest_sha256}],
    corpora:[...old.manifest.corpora.map(c=>({...c,country_version:'original'})),...fresh.manifest.corpora.map(c=>({...c,country_version:'enriched'}))],
    artifacts:list,files:list.length,bytes:list.reduce((sum,a)=>sum+a.bytes,0)};
  await fs.writeFile(path.join(out,'export-manifest.json'),JSON.stringify(manifest)+'\n');
  return {output:out,files:manifest.files,bytes:manifest.bytes,country_versions:countryVersionInfo,default_country_version:'enriched',source_exports:manifest.source_exports};
}

if(process.argv[1]&&import.meta.url===pathToFileURL(path.resolve(process.argv[1])).href){
  const options={};
  for(let i=2;i<process.argv.length;i+=2){const key=process.argv[i];if(!['--original','--enriched','--output'].includes(key)||!process.argv[i+1]||options[key.slice(2)])throw new Error('Usage: node scripts/combine-country-exports.mjs --original DIR --enriched DIR --output NEW_DIR');options[key.slice(2)]=process.argv[i+1];}
  console.log(JSON.stringify(await combineCountryExports(options),null,2));
}
