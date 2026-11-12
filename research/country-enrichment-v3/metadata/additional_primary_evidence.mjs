// One bounded public representation attempt per independently selected DOI.
// No country/identity judgement, API credentials, challenge or paywall bypass.
import fs from 'node:fs/promises';
import {createHash} from 'node:crypto';
const base='research/country-enrichment-v3/metadata',out=base+'/crossref-additional-primary-paper';
await fs.mkdir(out,{recursive:true});
const requests=JSON.parse(await fs.readFile(base+'/crossref-additional-primary-requests.json','utf8'));
const observed=JSON.parse(await fs.readFile(base+'/crossref-observed-primary-links.json','utf8'));
const hash=x=>createHash('sha256').update(x).digest('hex');
const deny=u=>/(^|\.)(?:crossref\.org|ncbi\.nlm\.nih\.gov)$/.test(u.hostname)||!['https:','http:'].includes(u.protocol)||u.username||u.password||/localhost|^\d+\.\d+\.\d+\.\d+$/.test(u.hostname);
async function redirectRequest(url,method,signal){const redirects=[];
 for(let n=0;n<5;n++){
  const u=new URL(url);if(deny(u))throw new Error('Excluded API/credential/nonpublic destination');
  const r=await fetch(u,{method,redirect:'manual',signal,headers:{accept:'text/html,application/xml,text/plain,application/pdf;q=0.8'}});
  if(r.status>=300&&r.status<400&&r.headers.get('location')){const next=new URL(r.headers.get('location'),u).href;redirects.push({url:u.href,status:r.status,location:next});await r.body?.cancel();url=next;continue;}
  return {response:r,url:u.href,redirects};
 }throw new Error('Bounded redirect limit');
}
async function mdpiCanonical(doi){let url='https://doi.org/'+doi;const signal=AbortSignal.timeout(15000);
 for(let n=0;n<4;n++){
  const u=new URL(url);if(!u.hostname.endsWith('doi.org'))return u.href;
  const r=await fetch(u,{method:'HEAD',redirect:'manual',signal,headers:{accept:'text/html'}});const next=r.headers.get('location');await r.body?.cancel();if(!next)throw new Error('Resolver HEAD '+r.status);url=new URL(next,u).href;
 }throw new Error('Resolver redirect bound');
}
function exporter(doi,canonical){const q=encodeURIComponent(doi);const prefix=doi.split('/')[0];
 const platforms={'10.1080':'https://www.tandfonline.com','10.1177':'https://journals.sagepub.com','10.1142':'https://www.worldscientific.com','10.1155':'https://onlinelibrary.wiley.com','10.1021':'https://pubs.acs.org','10.1098':'https://royalsocietypublishing.org','10.1126':'https://www.science.org'};
 if(platforms[prefix])return {kind:'official_public_citation_export',url:platforms[prefix]+'/action/downloadCitation?doi='+q+'&format=bibtex&include=abs&direct=true'};
 if(prefix==='10.1145')return {kind:'official_public_citation_export',url:'https://dl.acm.org/action/exportCiteProcCitation?dois='+q+'&targetFile=custom-bibtex&format=bibTex'};
 if(prefix==='10.1109'&&canonical){const id=new URL(canonical).pathname.match(/\/document\/(\d+)/)?.[1];if(id)return {kind:'official_public_citation_export',url:'https://ieeexplore.ieee.org/xpl/downloadCitations?recordIds='+id+'&download-format=download-bibtex&citations-format=citation-only'};}
 if(prefix==='10.1093'&&canonical){const u=new URL(canonical);const ids=u.pathname.match(/\/(\d{5,})(?:\/[^/]*)?\/?$/);if(ids&&u.hostname==='academic.oup.com')return {kind:'official_public_citation_export',url:'https://academic.oup.com/citation/download?resourceId='+ids[1]+'&resourceType=3&citationFormat=2'};}
 if(prefix==='10.1590'&&canonical&&new URL(canonical).hostname.endsWith('scielo.br')){const u=new URL(canonical);u.searchParams.set('format','xml');return {kind:'official_public_xml_representation',url:u.href};}
 return null;
}
let cursor=0;const results=[];const start=Date.now();
async function worker(){while(cursor<requests.records.length){const r=requests.records[cursor++];const t=Date.now();let record={id:r.id,doi:r.doi};
 try{
  const old=observed[r.doi]||{};let canonical=old.final_url;let plan;
  if(r.doi.startsWith('10.3390/')){
   if(!canonical&&old.http_status!==403)canonical=await mdpiCanonical(r.doi);
   if(canonical&&new URL(canonical).hostname.endsWith('mdpi.com')){const u=new URL(canonical);u.search='';u.hash='';u.pathname=u.pathname.replace(/\/$/,'')+'/xml';plan={kind:'official_public_xml_representation',url:u.href};}
  }
  if(!plan&&old.observed_public_representation_links?.length)plan={kind:'representation_link_observed_on_official_html',url:old.observed_public_representation_links[0]};
  if(!plan)plan=exporter(r.doi,canonical);
  if(!plan){record.status='unavailable_no_observed_or_known_public_export';results.push(record);continue;}
  record={...record,...plan};const controller=new AbortController();const timeout=setTimeout(()=>controller.abort(),20000);let rr;
  try{
   rr=await redirectRequest(plan.url,'GET',controller.signal);const chunks=[];let bytes=0,truncated=false;
   if(rr.response.body){for await(const chunk of rr.response.body){const available=8*1024*1024-bytes;if(chunk.length>available){chunks.push(chunk.subarray(0,available));bytes+=available;truncated=true;controller.abort();break;}chunks.push(chunk);bytes+=chunk.length;}}
   const body=Buffer.concat(chunks);const path=out+'/'+r.id+'-'+hash(plan.url).slice(0,12)+'.bin';await fs.writeFile(path,body);
   record={...record,status:'public_response_captured_no_identity_judgement',http_status:rr.response.status,final_url:rr.url,redirects:rr.redirects,content_type:rr.response.headers.get('content-type'),bytes:body.length,sha256:hash(body),path,truncated};
  }finally{clearTimeout(timeout);}
 }catch(e){record.status='unavailable_error';record.error=e.message;}
 record.seconds=(Date.now()-t)/1000;results.push(record);console.log(JSON.stringify({id:r.id,status:record.status,http_status:record.http_status,done:results.length,total:requests.records.length}));
}}
await Promise.all(Array.from({length:6},worker));
const manifest={status:'bounded_public_primary_paper_evidence_attempts_complete',requested:requests.records.length,records:results,seconds:(Date.now()-start)/1000,network_workers:6,max_bytes_per_response:8*1024*1024,timeout_seconds:20,max_attempts_per_doi:1,no_country_or_identity_judgements:true,no_crossref_or_pubmed_metadata_api:true,no_credentials_cookies_paywall_or_challenge_bypass:true,request_sha256:hash(await fs.readFile(base+'/crossref-additional-primary-requests.json'))};
await fs.writeFile(out+'/manifest.json',JSON.stringify(manifest,null,2)+'\n');
