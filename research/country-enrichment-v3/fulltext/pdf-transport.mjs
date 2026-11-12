// Ordinary public OA retrieval only. Frozen source membership/pi2 never changes.
import fs from 'node:fs/promises';
import path from 'node:path';
import dns from 'node:dns/promises';
import net from 'node:net';
import {createHash} from 'node:crypto';
import {createRequire} from 'node:module';
const require=createRequire(import.meta.url),u=require(path.resolve('bin/cloudflare-runtime/node_modules/undici'));
const dispatcher=new u.EnvHttpProxyAgent();
const [planPath,rootPath]=process.argv.slice(2);if(!planPath||!rootPath)throw Error('usage: pdf-transport plan.json root');
const planBytes=await fs.readFile(planPath),plan=JSON.parse(planBytes),sha=createHash('sha256').update(planBytes).digest('hex'),root=path.resolve(rootPath),max=20*1024*1024;
await fs.mkdir(path.join(root,'pdf'),{recursive:true});await fs.mkdir(path.join(root,'transport'),{recursive:true});
function privateIP(ip){
 ip=ip.toLowerCase().replace(/^\[|\]$/g,'');if(ip.startsWith('::ffff:'))return privateIP(ip.slice(7));
 if(net.isIP(ip)===4){const x=ip.split('.').map(Number);return x[0]===0||x[0]===10||x[0]===127||x[0]>=224||(x[0]===169&&x[1]===254)||(x[0]===172&&x[1]>=16&&x[1]<=31)||(x[0]===192&&x[1]===168)||(x[0]===100&&x[1]>=64&&x[1]<=127)||(x[0]===198&&[18,19].includes(x[1]))}
 return ip==='::1'||ip==='::'||ip.startsWith('fc')||ip.startsWith('fd')||ip.startsWith('fe8')||ip.startsWith('ff');
}
const dnsCache=new Map();
async function publicURL(raw){
 const url=new URL(raw),host=url.hostname.replace(/^\[|\]$/g,'').toLowerCase();
 if(!['http:','https:'].includes(url.protocol)||url.username||url.password||host==='localhost'||host.endsWith('.local')||host.endsWith('.internal'))throw Error('nonpublic_source');
 if(net.isIP(host)){if(privateIP(host))throw Error('nonpublic_source_ip')}
 else{
  if(!dnsCache.has(host))dnsCache.set(host,dns.lookup(host,{all:true}).then(rows=>{if(!rows.length||rows.some(x=>privateIP(x.address)))throw Error('nonpublic_source_dns');return true}));
  await dnsCache.get(host);
 }
 return url;
}
let cursor=0;
await Promise.all(Array.from({length:3},async()=>{while(cursor<plan.pdf.length){
 const pick=plan.pdf[cursor++],destination=path.join(root,'transport',`W${pick.id}.json`);
 try{const old=JSON.parse(await fs.readFile(destination));if(old.plan_sha256===sha)continue}catch{}
 const begin=performance.now(),o={id:pick.id,url:pick.url,pi2:pick.pi2,stratum:pick.stratum,plan_sha256:sha,policy:'ordinary_public_oa_get_no_metadata_api',started_at:new Date().toISOString(),status:'running',bytes:0};
 try{
  let url=await publicURL(pick.url),response,signal=AbortSignal.timeout(55000);
  for(let hop=0;hop<=6;hop++){
   response=await u.fetch(url,{dispatcher,redirect:'manual',signal,headers:{'user-agent':'OpenAlex-country-holdout/3.0 (bounded academic study)'}});
   if([301,302,303,307,308].includes(response.status)){
    const location=response.headers.get('location');await response.body?.cancel();if(!location||hop===6)throw Error('redirect_limit_or_missing_location');url=await publicURL(new URL(location,url));continue;
   }
   break;
  }
  o.http_status=response.status;o.final_url=response.url;o.content_type=response.headers.get('content-type');
  if(!response.ok){o.status='http_error';await response.body?.cancel()}
  else if(Number(response.headers.get('content-length')||0)>max){o.status='content_length_exceeds_budget';await response.body?.cancel()}
  else{
   const chunks=[];for await(const chunk of response.body){o.bytes+=chunk.length;if(o.bytes>max)throw Error('body_exceeds_budget');chunks.push(chunk)}
   const b=Buffer.concat(chunks);o.sha256=createHash('sha256').update(b).digest('hex');o.pdf_magic=b.subarray(0,1024).includes(Buffer.from('%PDF-'));o.status=o.pdf_magic?'pdf_reached':'not_pdf';
   o.path=path.join(root,'pdf',`W${pick.id}.pdf`);await fs.writeFile(o.path,b);
  }
 }catch(error){o.status='transport_error';o.error=String(error?.message||error).replace(/https?:\/\/[^\s/@]+:[^\s/@]+@/g,'[redacted-proxy]')}
 o.seconds=(performance.now()-begin)/1000;await fs.writeFile(destination,JSON.stringify(o,null,2)+'\n');
 console.log(JSON.stringify({id:o.id,status:o.status,http:o.http_status,bytes:o.bytes}));
}}));
await dispatcher.close();const records=[];for(const x of plan.pdf)records.push(JSON.parse(await fs.readFile(path.join(root,'transport',`W${x.id}.json`))));
await fs.writeFile(path.join(root,'transport-results.json'),JSON.stringify({status:'complete_for_frozen_plan',plan_sha256:sha,selected:records.length,workers:3,records},null,2)+'\n');
