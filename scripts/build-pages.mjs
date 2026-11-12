import fs from 'node:fs/promises';
import path from 'node:path';
import {createHash} from 'node:crypto';
const root=process.cwd(),output=path.join(root,'dist'),source=path.join(root,'cloudflare','data');
const manifest=JSON.parse(await fs.readFile(path.join(source,'export-manifest.json'),'utf8'));
if(manifest.version!=='openalex-static-query-v1')throw new Error('Unsupported data export; run oa-stats export-site.');
// Only remove the explicitly resolved project build output, never user input.
if(path.dirname(output)!==root||path.basename(output)!=='dist')throw new Error('Unsafe build output');
await fs.rm(output,{recursive:true,force:true});await fs.mkdir(output,{recursive:true});
for(const artifact of manifest.artifacts){
  const relative=artifact.path,target=path.resolve(source,relative);
  if(!target.startsWith(source+path.sep))throw new Error('Invalid data manifest path');
  const bytes=await fs.readFile(target);
  if(bytes.length!==artifact.bytes||createHash('sha256').update(bytes).digest('hex')!==artifact.sha256)throw new Error(`Data checksum mismatch: ${relative}`);
  if(bytes.length>25*1024*1024)throw new Error(`Cloudflare per-file limit exceeded: ${relative}`);
  const destination=path.join(output,relative);await fs.mkdir(path.dirname(destination),{recursive:true});await fs.writeFile(destination,bytes);
}
await fs.cp(path.join(root,'internal','web','static'),path.join(output,'static'),{recursive:true});
await fs.copyFile(path.join(root,'internal','web','static','index.html'),path.join(output,'index.html'));
await fs.copyFile(path.join(root,'cloudflare','worker.mjs'),path.join(output,'_worker.js'));
await fs.writeFile(path.join(output,'_routes.json'),JSON.stringify({version:1,include:['/api/*','/download'],exclude:[]},null,2)+'\n');
await fs.writeFile(path.join(output,'404.html'),'<!doctype html><meta charset="utf-8"><title>404</title><p>页面不存在。<a href="/">返回统计界面</a></p>\n');
await fs.writeFile(path.join(output,'_headers'),`/*\n  X-Content-Type-Options: nosniff\n  Referrer-Policy: no-referrer\n  Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'\n/static/*\n  Cache-Control: public, max-age=3600\n/data/site.json\n  Cache-Control: no-cache\n`);
let count=0,total=0,max=0;async function walk(directory){for(const entry of await fs.readdir(directory,{withFileTypes:true})){const name=path.join(directory,entry.name);if(entry.isDirectory())await walk(name);else{const {size}=await fs.stat(name);count++;total+=size;max=Math.max(max,size);}}}await walk(output);
if(count>20000||max>25*1024*1024)throw new Error('Cloudflare free static-asset limits exceeded');
console.log(JSON.stringify({output:'dist',files:count,bytes:total,max_file_bytes:max,default_country_version:manifest.default_country_version||'original',datasets:manifest.corpora.map(c=>({corpus:c.corpus,country_version:c.country_version||'original',run_id:c.run_id}))},null,2));
