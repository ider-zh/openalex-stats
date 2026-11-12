import {test} from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import {tmpdir} from 'node:os';
import {createHash} from 'node:crypto';
import {combineCountryExports} from './combine-country-exports.mjs';

const hash=bytes=>createHash('sha256').update(bytes).digest('hex');
async function workspace(t){
  const base=await fs.realpath(tmpdir()),directory=await fs.mkdtemp(path.join(base,'openalex-country-combine-'));
  t.after(async()=>{assert.equal(path.dirname(directory),base);assert.ok(path.basename(directory).startsWith('openalex-country-combine-'));await fs.rm(directory,{recursive:true,force:true});});
  return directory;
}
async function fixture(root,id,{shared}={}){
  await fs.mkdir(root);const artifacts=[],corpora=[],datasets={};
  async function write(name,content){const bytes=Buffer.from(content),p=path.join(root,name);await fs.mkdir(path.dirname(p),{recursive:true});await fs.writeFile(p,bytes);const entry={path:name,bytes:bytes.length,sha256:hash(bytes)};artifacts.push(entry);return entry;}
  for(const corpus of ['core','all']){
    const prefix=`/data/${id}-${corpus}`,entry=await write(prefix.slice(1)+'/counts/0.bin.gz',`${id}-${corpus}-exact-bytes`);
    const run_id=`${id}-${corpus}-run`,completion_sha256=`${id}-${corpus}-completion`;
    datasets[corpus]={prefix,run_id,completion_sha256,meta:{corpus,selected_works:100,snapshot_date:'2026-06-26',year_start:2000,year_end:2026},csv_header:['country','value']};
    corpora.push({corpus,run_id,completion_sha256,artifacts:[entry]});
  }
  if(shared)await write('data/shared.bin',shared);
  await write('data/site.json',JSON.stringify({version:'openalex-static-query-v1',catalog:{default_corpus:'core',datasets:[{id:'core'},{id:'all'}]},datasets}));
  const manifest={version:'openalex-static-query-v1',corpora,artifacts,files:artifacts.length,bytes:artifacts.reduce((n,a)=>n+a.bytes,0)};
  await fs.writeFile(path.join(root,'export-manifest.json'),JSON.stringify(manifest));return manifest;
}

test('combine verifies both complete sources, preserves scientific bytes and binds all controls to country version',async t=>{
  const base=await workspace(t),original=path.join(base,'original'),enriched=path.join(base,'enriched'),output=path.join(base,'combined');
  const old=await fixture(original,'original'),fresh=await fixture(enriched,'enriched');
  const before=await fs.readFile(path.join(original,'data/site.json'));
  const report=await combineCountryExports({original,enriched,output});assert.equal(report.default_country_version,'enriched');assert.equal(report.files,5);
  assert.deepEqual(await fs.readFile(path.join(original,'data/site.json')),before);
  const site=JSON.parse(await fs.readFile(path.join(output,'data/site.json')));assert.equal(site.catalog.default_country_version,'enriched');assert.deepEqual(site.datasets,site.country_versions.enriched);assert.equal(site.country_versions.original.core.meta.country_version,'original');assert.equal(site.country_versions.enriched.all.meta.country_version_info.version,'country-enriched-v3-r3');assert.deepEqual(site.datasets.core.csv_header,['country','value','country_version']);
  const manifest=JSON.parse(await fs.readFile(path.join(output,'export-manifest.json')));assert.equal(manifest.corpora.length,4);assert.deepEqual(manifest.corpora.map(x=>x.country_version),['original','original','enriched','enriched']);
  for(const source of [old,fresh])for(const item of source.artifacts)if(item.path!=='data/site.json')assert.equal(hash(await fs.readFile(path.join(output,item.path))),item.sha256);
  for(const item of manifest.artifacts){const bytes=await fs.readFile(path.join(output,item.path));assert.equal(bytes.length,item.bytes);assert.equal(hash(bytes),item.sha256);}
});

test('combine rejects corrupt source, collisions and in-place output before creating destination',async t=>{
  const base=await workspace(t),original=path.join(base,'original'),enriched=path.join(base,'enriched');await fixture(original,'original',{shared:'old'});await fixture(enriched,'enriched',{shared:'new'});
  const output=path.join(base,'conflict');await assert.rejects(combineCountryExports({original,enriched,output}),/Conflicting export path/);await assert.rejects(fs.stat(output),{code:'ENOENT'});
  await assert.rejects(combineCountryExports({original,enriched,output:path.join(original,'nested')}),/outside both inputs/);
  await fs.writeFile(path.join(original,'data/original-core/counts/0.bin.gz'),'tampered');
  const damaged=path.join(base,'damaged');await assert.rejects(combineCountryExports({original,enriched,output:damaged}),/Source checksum mismatch/);await assert.rejects(fs.stat(damaged),{code:'ENOENT'});
});
