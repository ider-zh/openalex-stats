// Bounded mature-curl bulk transport. No source interpretation or metadata API.
import fs from 'node:fs/promises';
import {createReadStream} from 'node:fs';
import {spawn} from 'node:child_process';
import {pipeline} from 'node:stream/promises';
import {createHash} from 'node:crypto';
const dir='research/country-enrichment-v3/metadata';
const plan=JSON.parse(await fs.readFile(dir+'/download-plan.json','utf8'));
const files=plan.selected||plan.files;const parts=dir+'/pubmed-curl-parts';await fs.mkdir(parts,{recursive:true});
function command(cmd,args,stdin){return new Promise((resolve,reject)=>{
 const p=spawn(cmd,args,{stdio:[stdin?'pipe':'ignore','pipe','pipe']});let out='',err='';p.stdout.on('data',x=>out+=x);p.stderr.on('data',x=>err+=x);p.on('error',reject);const exited=new Promise((r,j)=>p.once('close',code=>code===0?r():j(new Error(cmd+' exit '+code+' '+err.slice(-1500)))));
 (async()=>{try{await Promise.all([exited,stdin?pipeline(stdin,p.stdin):Promise.resolve()]);resolve(out);}catch(e){p.kill();reject(e);}})();
});}
const remote=JSON.parse(await command('ssh',['-o','BatchMode=yes','ider@192.168.1.229',"python3 -c 'import glob,json;print(json.dumps([json.load(open(p)) for p in glob.glob(\"/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112/metadata/dumps/pubmed/*.bridge-verified.json\")]))'"]));
const byName=new Map(remote.map(x=>[x.name,x]));
let cursor=0;const results=[];const started=Date.now();
async function digest(path,kind){const h=createHash(kind);for await(const b of createReadStream(path))h.update(b);return h.digest('hex');}
async function worker(){while(cursor<files.length){const f=files[cursor++];if(!/^pubmed26n\d{4}\.xml\.gz$/.test(f.name))throw new Error('Invalid frozen name');const evidence=dir+'/pubmed-transfers/'+f.name+'.json';const old=byName.get(f.name);
 if(old?.status==='passed'&&old.bytes===f.source_bytes){const r={...f,status:'passed',bytes:old.bytes,source_md5:old.md5,sha256:old.sha256,remote_verification:old,reused:true};await fs.writeFile(evidence,JSON.stringify(r,null,2)+'\n');results.push(r);continue;}
 const path=parts+'/'+f.name+'.partial';let done=false,lastError='';const url='https://ftp.ncbi.nlm.nih.gov/pubmed/baseline/'+f.name;
 for(let attempt=1;attempt<=4&&!done;attempt++){try{
  const check=await fetch(url+'.md5?bulk-retry=v3',{signal:AbortSignal.timeout(30000)});if(!check.ok)throw new Error('MD5 HTTP '+check.status);const md5=(await check.text()).match(/[a-f0-9]{32}/i)?.[0].toLowerCase();if(!md5)throw new Error('No official MD5');
  let size=0;try{size=(await fs.stat(path)).size;}catch{}
  if(size>f.source_bytes)throw new Error('Local partial larger than frozen source');
  if(size!==f.source_bytes)await command('curl.exe',['--silent','--show-error','--fail','--location','--continue-at','-','--max-time','300','--speed-limit','50000','--speed-time','45','--output',path,url+'?bulk-retry=v3']);
  const st=await fs.stat(path);if(st.size!==f.source_bytes||await digest(path,'md5')!==md5)throw new Error('Full file official checksum/size mismatch');
  const sha=await digest(path,'sha256');const proof=JSON.parse(await command('ssh',['-o','BatchMode=yes','ider@192.168.1.229','/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112/metadata/receive',f.name,md5],createReadStream(path)));
  if(proof.status!=='passed'||proof.bytes!==f.source_bytes||proof.md5!==md5||proof.sha256!==sha)throw new Error('Remote transport checksum mismatch');
  const r={...f,url,status:'passed',bytes:proof.bytes,source_md5:md5,sha256:sha,remote_verification:proof,attempt,transport:'Six-worker native curl resumable official bulk object; full MD5+SHA256 then SSH checksum verification',per_work_api_calls:0};await fs.writeFile(evidence,JSON.stringify(r,null,2)+'\n');results.push(r);await fs.unlink(path);done=true;console.log(JSON.stringify({name:f.name,done:results.length,total:files.length,seconds:(Date.now()-started)/1000}));
 }catch(e){lastError=e.message;console.log(JSON.stringify({name:f.name,attempt,error:lastError}));}}
 if(!done){const r={...f,status:'failed',error:lastError};results.push(r);await fs.writeFile(evidence,JSON.stringify(r,null,2)+'\n');}
}}
await Promise.all(Array.from({length:6},worker));
await fs.writeFile(dir+'/pubmed-curl-bridge-latest.json',JSON.stringify({status:results.every(x=>x.status==='passed')?'passed':'partial',results,elapsed_seconds:(Date.now()-started)/1000,concurrent_transfers:6,per_work_api_calls:0},null,2)+'\n');
