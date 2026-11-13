// Run immediately after workerd startup, before model API requests warm caches.
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
const edge=process.env.PAGES_URL||'http://127.0.0.1:8799',native=process.env.GO_URL||'http://192.168.1.229:8898';
const output=path.resolve(process.env.PARITY_OUTPUT||'research/longtail-patch-v2/parity/workerd-cold-concurrency.json');
const query=new URLSearchParams({country_version:'enriched',corpus:'core',complexity_profile:'p1000-c1000-x5-d3-v1',method:'java-reflections-v1',scheme:'topics',level:'field',metric:'eci',basis:'publications',countries:'CN,US,GB,AG,EH,SG',start:'2000',end:'2026'}),url='/api/series?'+query;
const report={status:'running',edge,native,started_at_utc:new Date().toISOString(),parallel_requests:12,passes:0,policy:'Twelve simultaneous identical requests on freshly restarted workerd, before profile/model cache warmup; exact Go response equality. Global cache may share only completed JS objects, never pending I/O.'};
try{
  const reference=await fetch(native+url,{signal:AbortSignal.timeout(60000)});assert.equal(reference.status,200);const expected=await reference.json();
  const results=await Promise.allSettled(Array.from({length:12},async()=>{const r=await fetch(edge+url,{signal:AbortSignal.timeout(60000)}),text=await r.text();assert.equal(r.status,200,text.slice(0,600));assert.deepEqual(JSON.parse(text),expected);report.passes++;}));
  const failed=results.find(r=>r.status==='rejected');if(failed)throw failed.reason;report.status='passed';
}catch(error){report.status='failed';report.failure=String(error.stack||error).slice(0,8000);process.exitCode=1;}
finally{report.completed_at_utc=new Date().toISOString();await fs.mkdir(path.dirname(output),{recursive:true});await fs.writeFile(output,JSON.stringify(report,null,2)+'\n');console.log(JSON.stringify(report,null,2));}
