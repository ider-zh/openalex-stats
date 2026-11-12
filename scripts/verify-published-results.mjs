// Verify public availability without credentials after the uploader checks server SHA.
import fs from 'node:fs/promises';
import {createHash} from 'node:crypto';
import assert from 'node:assert/strict';
const index=JSON.parse(await fs.readFile(new URL('../results/index.json',import.meta.url),'utf8'));
assert.equal(index.status,'published');
const report={status:'running',started_at_utc:new Date().toISOString(),authentication:false,checks:[]};
try{
  let cursor=0;
  await Promise.all(Array.from({length:3},async()=>{
    while(cursor<index.assets.length){
      const asset=index.assets[cursor++],url=new URL(asset.url);
      assert.equal(url.origin,'https://github.com');
      assert.ok(url.pathname.startsWith('/ider-zh/openalex-stats/releases/download/'));
      const response=await fetch(url,{method:asset.bytes<=5*1024*1024?'GET':'HEAD'});
      assert.equal(response.status,200,asset.name);
      const check={name:asset.name,bytes:asset.bytes,status:response.status,check:'public-head-size; full SHA verified by GitHub upload response'};
      assert.equal(Number(response.headers.get('content-length')),asset.bytes,asset.name);
      if(asset.bytes<=5*1024*1024){
        const bytes=new Uint8Array(await response.arrayBuffer());assert.equal(bytes.length,asset.bytes);
        assert.equal(createHash('sha256').update(bytes).digest('hex'),asset.sha256,asset.name);
        check.check='public-full-download-size-and-sha256';
      }
      report.checks.push(check);
    }
  }));
  for(const corpus of ['core','all']){
    const asset=index.assets.find(a=>a.corpus===corpus&&a.filename==='statistics.parquet');
    const response=await fetch(asset.url,{headers:{Range:'bytes=0-3'}});
    if(response.status!==206){await response.body?.cancel();throw new Error(`Public range unsupported: ${asset.name} ${response.status}`);}
    assert.equal(Buffer.from(await response.arrayBuffer()).toString(),'PAR1');
    report.checks.push({name:asset.name,status:206,check:'public-range-parquet-magic'});
  }
  report.checks.sort((a,b)=>a.name.localeCompare(b.name));report.status='passed';
}catch(error){report.status='failed';report.failure=String(error.stack||error);process.exitCode=1;}
finally{
  report.completed_at_utc=new Date().toISOString();
  await fs.writeFile('research/cloudflare-delivery/public-download-verification.json',JSON.stringify(report,null,2)+'\n');
  console.log(JSON.stringify({status:report.status,checks:report.checks.length,failure:report.failure}));
}
