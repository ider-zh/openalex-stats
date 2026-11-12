// Transport only: static official PMC OA objects, no country/identity decisions.
import fs from 'node:fs/promises';
import path from 'node:path';
import {createHash} from 'node:crypto';
import {createRequire} from 'node:module';
const require=createRequire(import.meta.url);
const u=require(path.resolve('bin/cloudflare-runtime/node_modules/undici'));
const dispatcher=new u.EnvHttpProxyAgent();
const [planPath,cacheRoot,workerArg]=process.argv.slice(2),workers=Number(workerArg??3);
if(!planPath||!cacheRoot)throw Error('usage: node pmc-transport.mjs xml-plan.json cache-root');
if(!Number.isInteger(workers)||workers<1||workers>64)throw Error('bounded S3 concurrency must be 1..64');
const planBytes=await fs.readFile(planPath),plan=JSON.parse(planBytes),planSHA=createHash('sha256').update(planBytes).digest('hex');
const root=path.resolve(cacheRoot);await fs.mkdir(root,{recursive:true});
const runStarted=new Date().toISOString(),runStart=performance.now();let fetched=0,cached=0,failed=0,bytesTransferred=0,activeLimit=workers,cooldownUntil=0;
const recentErrors=[];
await fs.appendFile(path.join(root,'transport-runs.jsonl'),JSON.stringify({event:'started',utc:runStarted,workers,plan_sha256:planSHA,transport_only_override:'Root authorized bounded static S3 concurrency benchmark up to64; identity/country/selection/MD5/retry policy unchanged'})+'\n');
for(const d of ['pmc-metadata','xml','transport'])await fs.mkdir(path.join(root,d),{recursive:true});
function official(raw){
 raw=raw.replace(/^s3:\/\/pmc-oa-opendata\//,'https://pmc-oa-opendata.s3.amazonaws.com/');
 const url=new URL(raw);if(url.protocol!=='https:'||url.hostname!=='pmc-oa-opendata.s3.amazonaws.com'||url.username||url.password)throw Error('nonofficial PMC object');return url;
}
async function fetchObject(raw,destination,max){
 const url=official(raw),start=performance.now();let record={url:url.href,path:destination,status:'running',bytes:0,plan_sha256:planSHA};
 try{
  let bytes;
  try{bytes=await fs.readFile(destination);record.cached=true;}catch{}
  if(!bytes){
   const response=await u.fetch(url,{dispatcher,redirect:'manual',signal:AbortSignal.timeout(55000),headers:{'user-agent':'OpenAlex-country-holdout/3.0 (bounded public PMC object retrieval)'}});
   record.http_status=response.status;
   if(!response.ok){await response.body?.cancel();if(response.status===429||response.status===503){activeLimit=Math.min(activeLimit,16);cooldownUntil=Math.max(cooldownUntil,Date.now()+10000);await fs.appendFile(path.join(root,'transport-runs.jsonl'),JSON.stringify({event:'global_backoff',http_status:response.status,active_workers:activeLimit,utc:new Date().toISOString()})+'\n');await new Promise(r=>setTimeout(r,10000));}throw Error('HTTP '+response.status)}
   if(Number(response.headers.get('content-length')||0)>max){await response.body?.cancel();throw Error('content_length_exceeds_limit')}
   const chunks=[];for await(const chunk of response.body){record.bytes+=chunk.length;if(record.bytes>max){await response.body?.cancel();throw Error('body_exceeds_limit')}chunks.push(chunk)}bytes=Buffer.concat(chunks);
  }
  record.bytes=bytes.length;record.sha256=createHash('sha256').update(bytes).digest('hex');
  const expected=url.searchParams.get('md5');if(expected&&createHash('md5').update(bytes).digest('hex')!==expected.toLowerCase())throw Error('official_md5_mismatch');
  record.official_md5=expected;record.status='reached';record.http_status=200;
  if(!record.cached){await fs.writeFile(destination+'.tmp',bytes);await fs.rename(destination+'.tmp',destination)}
  return {record,bytes};
 }catch(error){record.status='failed';record.error=String(error.message);return {record};}
 finally{record.seconds=(performance.now()-start)/1000;record.workers=workers;record.active_workers=activeLimit;record.run_started=runStarted;if(record.status==='failed')failed++;else if(record.cached)cached++;else{fetched++;bytesTransferred+=record.bytes;}if(!record.cached){recentErrors.push(record.status==='failed');if(recentErrors.length>100)recentErrors.shift();if(recentErrors.length>=20&&recentErrors.filter(Boolean).length/recentErrors.length>=.05){activeLimit=Math.min(activeLimit,16);cooldownUntil=Math.max(cooldownUntil,Date.now()+10000);}}await fs.appendFile(path.join(root,'fetch-events.jsonl'),JSON.stringify(record)+'\n');}
}
const taskMap=new Map();for(const work of plan.pmc??[])for(const key of work.inventory_metadata_keys??[]){if(!taskMap.has(key))taskMap.set(key,{ids:[],key});taskMap.get(key).ids.push(work.id)}
const tasks=[...taskMap.values()].sort((a,b)=>a.key.localeCompare(b.key));
let cursor=0;
await Promise.all(Array.from({length:workers},async(_,workerIndex)=>{while(cursor<tasks.length){
 if(workerIndex>=activeLimit||Date.now()<cooldownUntil){await new Promise(r=>setTimeout(r,500));continue;}
 const t=tasks[cursor++],name=path.basename(t.key),output=path.join(root,'transport',name+'.json');
 const metadata=await fetchObject('https://pmc-oa-opendata.s3.amazonaws.com/'+t.key,path.join(root,'pmc-metadata',name),2<<20);
 let xml;
 if(metadata.bytes){
  try{const m=JSON.parse(metadata.bytes);if(m.xml_url){const xname=path.basename(new URL(official(m.xml_url)).pathname);xml=await fetchObject(m.xml_url,path.join(root,'xml',xname),32<<20)}}catch(error){metadata.record.metadata_error=String(error.message)}
 }
 await fs.writeFile(output,JSON.stringify({ids:t.ids,key:t.key,metadata:metadata.record,xml:xml?.record,plan_sha256:planSHA},null,2)+'\n');
 if(cursor%50===0)console.log(JSON.stringify({completed:cursor,total:tasks.length,ids:t.ids,metadata:metadata.record.status,xml:xml?.record.status}));
}}));
await dispatcher.close();
const final={status:'complete_for_frozen_plan',plan_sha256:planSHA,tasks:tasks.length,workers,final_active_workers:activeLimit,metadata_api_calls:0,identity_or_country_inference:false,fetched,cached,failed,bytes_transferred:bytesTransferred,run_seconds:(performance.now()-runStart)/1000,started:runStarted};
await fs.writeFile(path.join(root,'transport-complete.json'),JSON.stringify(final,null,2)+'\n');
await fs.appendFile(path.join(root,'transport-runs.jsonl'),JSON.stringify({event:'completed',...final})+'\n');
