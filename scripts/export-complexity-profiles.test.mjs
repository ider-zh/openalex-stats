import {test} from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import {tmpdir} from 'node:os';
import {createHash} from 'node:crypto';
import {gunzipSync} from 'node:zlib';
import {encodeModelGroups,exportComplexityProfiles} from './export-complexity-profiles.mjs';

function fixture(){
  const meta={year_start:2000,year_end:2001,countries:[{id:'CN'},{id:'US'},{id:'GB'}],layers:[{scheme:'concepts',level:'0',labels:[{id:'d1'},{id:'d2'},{id:'d3'}]}]};
  const profile={id:'p1000-c1000-x5-d3-v1',country_min_publications:1000,country_min_citation_events:1000,cell_min_support:5,min_diversity:3},models=[],ranks=[],diagnostics=[];
  for(const year of [2000,2001])for(const metric of ['publications','citations']){
    // Native label order intentionally differs from website ordinal order.
    const coords={year,scheme:'concepts',level:'0',metric},result={countries:['GB','CN','US'],disciplines:['d3','d1','d2'],active_countries:[0,2],active_disciplines:[0,1],eci:[2,-2],pci:[.5,-.5],status:'ok',unstable:year===2001};
    models.push({...coords,result});ranks.push({...coords,country_ranks:[1,2],discipline_ranks:[1,2],country_universe:2,discipline_universe:2});
    diagnostics.push({...coords,diagnostics:{profile,algorithm:'country-scale-cell-zero-monotone-diversity-v1',countries:[{country:'CN',whole_publications:50,whole_citations:3000,original_matrix_mass:10,final_diversity:0,reasons:['country_min_publications']},{country:'US',whole_publications:5000,whole_citations:4000,original_matrix_mass:50,final_diversity:3,reasons:[]},{country:'GB',whole_publications:6000,whole_citations:7000,original_matrix_mass:80,final_diversity:3,reasons:[]}],rounds:[{round:1}],retained_mass_fraction:.9,source_matrix_sha256:'source',final_matrix_sha256:'final',final_network_sha256:'M'}});
  }
  return {models,ranks,diagnostics,meta,profile,profileRunID:'profile-run',sourceRunID:'source-run',sourceCompletionSHA256:'completion',scenario:'main'};
}

test('model encoder preserves native values/ranks and maps IDs to website ordinals with profile context',()=>{
  const f=fixture(),before=JSON.stringify(f),groups=encodeModelGroups(f);assert.equal(groups.length,2);assert.equal(JSON.stringify(f),before);
  const group=groups.find(g=>g.basis==='citations'),s=group.shard;
  assert.deepEqual(s.countries[0][0],[null,'filtered_country_publications',null,2,1,50,3000,0]);
  assert.deepEqual(s.countries[1][0],[-2,'ok',2,2,0,5000,4000,3]);
  assert.deepEqual(s.countries[2][1],[2,'unstable',1,2,0,6000,7000,3]);
  assert.deepEqual(s.disciplines[0][0],[-.5,'ok',2,2]);assert.deepEqual(s.disciplines[1][0],[null,'inactive',null,2]);assert.deepEqual(s.disciplines[2][0],[.5,'ok',1,2]);
  assert.equal(s.model_context.profile_run_id,'profile-run');assert.equal(s.model_context.source_completion_sha256,'completion');assert.equal(s.model_context.years.length,2);assert.equal(s.model_context.years[0].final_network_sha256,'M');
});

test('encoder rejects incomplete source/ranks, bad config and unknown exclusion reasons instead of inventing data',()=>{
  const missing=fixture();missing.ranks.pop();assert.throws(()=>encodeModelGroups(missing),/Incomplete/);
  const wrong=fixture();wrong.ranks[0].country_ranks=[1];assert.throws(()=>encodeModelGroups(wrong),/cardinality/);
  const config=fixture();config.diagnostics[0].diagnostics.profile={...config.profile,min_diversity:5};assert.throws(()=>encodeModelGroups(config),/configuration mismatch/);
  const reason=fixture();reason.diagnostics[0].diagnostics.countries[0].reasons=['unexpected'];assert.throws(()=>encodeModelGroups(reason),/Unknown country exclusion/);
});

test('complete static export keeps baseline bytes, native downloads and all dataset identities',async t=>{
  const temporary=await fs.realpath(tmpdir()),root=await fs.mkdtemp(path.join(temporary,'openalex-profile-export-'));
  t.after(async()=>{assert.equal(path.dirname(root),temporary);assert.ok(path.basename(root).startsWith('openalex-profile-export-'));await fs.rm(root,{recursive:true,force:true});});
  const hash=b=>createHash('sha256').update(b).digest('hex'),base=path.join(root,'base');await fs.mkdir(base);
  const site={version:'openalex-static-query-v1',catalog:{default_country_version:'enriched'},country_versions:{original:{},enriched:{}}},artifacts=[],bundles=[],rawModels=new Map();
  async function artifact(folder,relative,raw){const b=Buffer.from(raw),p=path.join(folder,relative);await fs.mkdir(path.dirname(p),{recursive:true});await fs.writeFile(p,b);return {path:relative,bytes:b.length,sha256:hash(b)};}
  for(const version of ['original','enriched'])for(const corpus of ['core','all']){
    const scope=version+'-'+corpus,sourceRun=hash(scope),completion=hash(scope+'completion'),f=fixture(),prefix='data/'+scope;
    artifacts.push(await artifact(base,prefix+'/baseline.gz','immutable-'+scope));
    const control={run_id:sourceRun,completion_sha256:completion,prefix:'/'+prefix,meta:{...f.meta,eci_scenario_id:'main'},csv_header:['country_version'],models:{baseline:'/'+prefix+'/baseline.gz'}};site.country_versions[version][corpus]=control;
    const bundle=path.join(root,scope);await fs.mkdir(bundle);bundles.push(bundle);
    const diagnostics=await artifact(bundle,'profile/diagnostics.json',JSON.stringify(f.diagnostics)),sourceManifest=await artifact(bundle,'profile/sources.json',JSON.stringify({source_run_id:sourceRun}));
    const entry={profile:f.profile,profile_run_id:hash(scope+'profile'),models:{},ranks:{},diagnostics,downloads:{shared:{'profile-diagnostics.json':diagnostics,'model_diagnostics.json':diagnostics,'profile-manifest.json':sourceManifest}}};
    for(const method of ['spectral-v1','java-reflections-v1']){
      const model=await artifact(bundle,method+'/models.json',JSON.stringify(f.models,null,2)+'\n');rawModels.set(scope+'/'+method,await fs.readFile(path.join(bundle,model.path)));entry.models[method]=model;entry.ranks[method]=await artifact(bundle,method+'/ranks.json',JSON.stringify(f.ranks));
      // The exporter copies opaque native Parquet bytes; no JS Parquet writer or solver.
      const country=await artifact(bundle,method+'/country_eci.parquet','native-country-'+scope),discipline=await artifact(bundle,method+'/discipline_pci.parquet','native-discipline-'+scope);
      entry.downloads[method]={'eci.json':model,'country_eci.parquet':country,'discipline_pci.parquet':discipline};
    }
    const baseline={profile:{id:'baseline-v1',display_name:'old label',country_min_publications:0,country_min_citation_events:0,cell_min_support:0,min_diversity:0}};
    await fs.writeFile(path.join(bundle,'manifest.json'),JSON.stringify({version:'complexity-filter-bundle-v1',direction_policy:'baseline-pooled-common-year-sign-v1',source_run_id:sourceRun,source_completion_sha256:completion,corpus,year_start:f.meta.year_start,year_end:f.meta.year_end,scenario_id:'main',default_profile_id:f.profile.id,profiles:[baseline,entry]}));
  }
  site.datasets=site.country_versions.enriched;const siteArtifact=await artifact(base,'data/site.json',JSON.stringify(site));artifacts.push(siteArtifact);
  await fs.writeFile(path.join(base,'export-manifest.json'),JSON.stringify({version:site.version,artifacts,files:artifacts.length,bytes:artifacts.reduce((s,a)=>s+a.bytes,0),corpora:[]}));
  const output=path.join(root,'output'),r=await exportComplexityProfiles({base,bundles,output});assert.equal(r.nonbaseline_profiles,1);assert.equal(r.baseline_scientific_artifacts_changed,0);
  assert.equal(hash(await fs.readFile(path.join(base,'data/site.json'))),siteArtifact.sha256);
  const manifest=JSON.parse(await fs.readFile(path.join(output,'export-manifest.json'))),control=JSON.parse(await fs.readFile(path.join(output,'data/site.json')));
  for(const a of manifest.artifacts){const raw=await fs.readFile(path.join(output,a.path));assert.equal(raw.length,a.bytes);assert.equal(hash(raw),a.sha256);}
  for(const a of artifacts)if(a.path!=='data/site.json')assert.equal(hash(await fs.readFile(path.join(output,a.path))),a.sha256);
  const ref=control.complexity_profile_manifests[fixture().profile.id],profileManifest=JSON.parse(await fs.readFile(path.join(output,ref.path.slice(1))));
  assert.equal(control.catalog.complexity_profiles[0].display_name,'不筛选 · 原模型');assert.equal(control.catalog.complexity_profiles[0].baseline,true);
  for(const version of ['original','enriched'])for(const corpus of ['core','all'])for(const method of ['spectral-v1','java-reflections-v1']){
    const dataset=profileManifest.datasets[version][corpus],download=dataset.downloads[method]['eci.json'],expected=rawModels.get(version+'-'+corpus+'/'+method);
    assert.equal(dataset.source_run_id,site.country_versions[version][corpus].run_id);assert.equal(download.encoding,'gzip');assert.equal(download.bytes,expected.length);assert.equal(download.sha256,hash(expected));assert.deepEqual(gunzipSync(await fs.readFile(path.join(output,download.path.slice(1)))),expected);
  }
  const oldFile=path.join(bundles[0],'manifest.json'),old=JSON.parse(await fs.readFile(oldFile));delete old.direction_policy;await fs.writeFile(oldFile,JSON.stringify(old));
  const rejected=path.join(root,'old-direction-rejected');await assert.rejects(exportComplexityProfiles({base,bundles,output:rejected}),/direction policy/);await assert.rejects(fs.stat(rejected),{code:'ENOENT'});
});
