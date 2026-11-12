// Download official bulk objects and stream to229; do not store full dump on C:.
import fs from 'node:fs/promises';
import {spawn} from 'node:child_process';
import {Readable,Transform} from 'node:stream';
import {pipeline} from 'node:stream/promises';
const dir='research/country-enrichment-v3/metadata';
const [planFile=dir+'/download-plan.json',limitArg]=process.argv.slice(2);
const plan=JSON.parse(await fs.readFile(planFile,'utf8'));
let files=plan.selected||plan.files;if(limitArg)files=files.slice(0,Number(limitArg));
let cursor=0,totalBytes=0;const started=Date.now(),results=[];
await fs.mkdir(dir+'/pubmed-transfers',{recursive:true});
async function worker(){while(cursor<files.length){const f=files[cursor++];if(!/^pubmed26n\d{4}\.xml\.gz$/.test(f.name))throw new Error('Invalid frozen filename');
 const evidence=dir+'/pubmed-transfers/'+f.name+'.json';try{const old=JSON.parse(await fs.readFile(evidence,'utf8'));if(old.status==='passed'){results.push({...old,reused:true});continue;}}catch{}
 const url='https://ftp.ncbi.nlm.nih.gov/pubmed/baseline/'+f.name;let succeeded=false,lastError;
 for(let attempt=1;attempt<=3&&!succeeded;attempt++){
  let child;
  try{
   const t=Date.now(),check=await fetch(url+'.md5',{signal:AbortSignal.timeout(30000)});if(!check.ok)throw new Error('MD5 HTTP'+check.status);const checksum=(await check.text()).match(/[a-f0-9]{32}/i)?.[0].toLowerCase();if(!checksum)throw new Error('Missing MD5');
   const response=await fetch(url,{signal:AbortSignal.timeout(20*60*1000)});if(!response.ok)throw new Error('Bulk HTTP'+response.status);
   child=spawn('ssh',['-o','BatchMode=yes','ider@192.168.1.229','/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112/metadata/receive',f.name,checksum],{stdio:['pipe','pipe','pipe']});
   let stdout='',stderr='',bytes=0;child.stdout.on('data',b=>stdout+=b);child.stderr.on('data',b=>stderr+=b);
   const ended=new Promise((resolve,reject)=>{child.once('error',reject);child.once('close',code=>code===0?resolve():reject(new Error('SSH receiver '+code+' '+stderr.slice(-1000))));});
   const meter=new Transform({transform(b,e,next){bytes+=b.length;totalBytes+=b.length;next(null,b);}});
   await Promise.all([pipeline(Readable.fromWeb(response.body),meter,child.stdin),ended]);
   const remote=JSON.parse(stdout);if(remote.status!=='passed'||remote.bytes!==bytes||remote.md5!==checksum)throw new Error('Transport verification mismatch');
   const result={...f,status:'passed',url,bytes,source_md5:checksum,sha256:remote.sha256,elapsed_seconds:(Date.now()-t)/1000,attempt,remote_verification:remote,per_work_api_calls:0};
   await fs.writeFile(evidence,JSON.stringify(result,null,2)+'\n');results.push(result);succeeded=true;
   console.log(JSON.stringify({name:f.name,status:'passed',bytes,seconds:result.elapsed_seconds,done:results.length,files:files.length,total_network_bytes:totalBytes,total_elapsed_seconds:(Date.now()-started)/1000}));
  }catch(e){lastError=e.message;child?.kill();if(attempt===3){const r={...f,status:'failed',error:lastError};results.push(r);await fs.writeFile(evidence,JSON.stringify(r,null,2)+'\n');}}
 }
}}
await Promise.all(Array.from({length:Math.min(6,files.length)},worker));
await fs.writeFile(dir+'/pubmed-bridge-'+(limitArg?'benchmark':'latest')+'.json',JSON.stringify({status:results.every(x=>x.status==='passed')?'passed':'partial',plan:planFile,results,network_bytes:totalBytes,elapsed_seconds:(Date.now()-started)/1000,concurrent_transfers:Math.min(6,files.length),per_work_api_calls:0},null,2)+'\n');
