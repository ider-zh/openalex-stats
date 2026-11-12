import fs from 'node:fs/promises';
import {createReadStream,createWriteStream,constants} from 'node:fs';
import path from 'node:path';
import {createHash,randomUUID} from 'node:crypto';
import {Readable,Transform} from 'node:stream';
import {pipeline} from 'node:stream/promises';
const params=new Map();for(let i=2;i<process.argv.length;i+=2){if(!['--corpus','--country-version','--directory','--file'].includes(process.argv[i])||!process.argv[i+1]||params.has(process.argv[i]))throw new Error('Usage: node scripts/download-results.mjs --corpus core|all [--country-version original|enriched] [--directory results/full] [--file asset-name]');params.set(process.argv[i],process.argv[i+1]);}
const corpus=params.get('--corpus');if(!['core','all'].includes(corpus))throw new Error('--corpus core|all is required');
const countryVersion=params.get('--country-version')||'original';
if(!['original','enriched'].includes(countryVersion))throw new Error('--country-version original|enriched is required');
const index=JSON.parse(await fs.readFile(new URL(countryVersion==='enriched'?'../results/country-enriched-v3-r3-index.json':'../results/index.json',import.meta.url),'utf8'));
if(index.status!=='published')throw new Error('Release asset publication has not been verified yet');
const root=path.resolve(params.get('--directory')||(countryVersion==='enriched'?'results/full/enriched':'results/full'));
const selected=index.assets.filter(a=>(a.corpus===corpus||a.group==='provenance')&&(!params.has('--file')||params.get('--file')===a.name));if(!selected.length)throw new Error('No matching result asset');
async function fileSHA(file){const h=createHash('sha256');for await(const b of createReadStream(file))h.update(b);return h.digest('hex');}
for(const asset of selected){
  if(!/^[A-Za-z0-9_.-]+$/.test(asset.filename)||!['base','methods','provenance'].includes(asset.group))throw new Error('Invalid result manifest');
  const dir=path.join(root,...(asset.group==='provenance'?['provenance']:[corpus,asset.group])),target=path.resolve(dir,asset.filename);if(!target.startsWith(root+path.sep))throw new Error('Unsafe output path');
  await fs.mkdir(dir,{recursive:true});let existing;try{existing=await fs.stat(target);}catch(error){if(error.code!=='ENOENT')throw error;}
  if(existing){if(existing.size!==asset.bytes||await fileSHA(target)!==asset.sha256)throw new Error(`Existing immutable file differs: ${target}`);console.log(`Verified ${path.relative(root,target)}`);continue;}
  const url=new URL(asset.url);if(url.protocol!=='https:'||url.hostname!=='github.com'||!url.pathname.startsWith('/ider-zh/openalex-stats/releases/download/'))throw new Error('Unexpected result URL');
  console.log(`Downloading ${asset.name} (${asset.bytes} bytes)`);const response=await fetch(url);if(!response.ok)throw new Error(`Download returned ${response.status}`);
  const temp=target+'.'+randomUUID()+'.partial',hash=createHash('sha256');let size=0;
  try{
    await pipeline(Readable.fromWeb(response.body),new Transform({transform(chunk,encoding,callback){size+=chunk.length;hash.update(chunk);callback(null,chunk);}}),createWriteStream(temp,{flags:'wx'}));
    if(size!==asset.bytes||hash.digest('hex')!==asset.sha256)throw new Error(`Checksum mismatch: ${asset.name}`);
    await fs.copyFile(temp,target,constants.COPYFILE_EXCL);await fs.unlink(temp);
  }catch(error){await fs.unlink(temp).catch(()=>{});throw error;}
  console.log(`Verified ${path.relative(root,target)}`);
}
