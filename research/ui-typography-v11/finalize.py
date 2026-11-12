import datetime,hashlib,json,os,runpy,urllib.request
from pathlib import Path
base=Path('/home/ider/tmpdata/openalex-stats-dev-20261112')
out=base/'ui-typography-v11'
helpers=runpy.run_path(str(out/'promote.py'))
state=json.loads((out/'promotion.json').read_text())
assert state['status']=='ready'
current=helpers['exact'](state['new'])
assert current['pid']==1172407 and current['start_clock_ticks']==5121844348
digest=lambda p:hashlib.sha256(Path(p).read_bytes()).hexdigest()
assert digest(current['exe'])=='9c0d2bd5b9a559a679a1b1a5ca3b6f8f64e48773c0eb5f4a8d7a4e612a26b00c'
checks=[]
for corpus in ['core','all']:
    path=out/f'production-{corpus}-browser-qa.json';r=json.loads(path.read_text())
    assert r['status']=='passed' and r['mode']=='formal' and r['dataset']['corpus']==corpus and len(r['checks'])==34
    assert not r['errors'] and not r['requests_outside_service'] and 'asset_override' not in r
    checks.append({'corpus':corpus,'checks':34,'path':str(path),'sha256':digest(path)})
visual_path=out/'typography-production-qa.json';visual=json.loads(visual_path.read_text())
assert visual['status']=='passed' and len(visual['checks'])==2 and visual['asset_override'] is False
assets={}
for name in ['app.css','app.js','bump.js']:
    with urllib.request.urlopen('http://127.0.0.1:8899/static/'+name) as response:
        data=response.read();assets[name]={'sha256':hashlib.sha256(data).hexdigest(),'bytes':len(data),'cache_control':response.headers['Cache-Control']}
candidate=helpers['process'](1171297)
assert candidate['exe']==current['exe'] and candidate['start_clock_ticks']==5121811773
assert candidate['argv'][-2:]==['--listen','0.0.0.0:8901']
helpers['stop'](candidate)
archive=base/'source-v11-typography-r2-release.tar.gz'
assert digest(archive)=='aaac074fe1cc9c6a7d1abe390cbf03bb17091dda551ac3fb257e76ae8d9aa27f'
record={'status':'passed','completed_at_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'url':'http://192.168.1.229:8899','production':current,'binary_sha256':digest(current['exe']),'source_archive':{'path':str(archive),'sha256':digest(archive),'isolated_from_country_enrichment':True,'independent_rebuild_matches_binary':True},'data':state['health'],'served_assets':assets,'formal_browser_qa':checks,'typography_qa':{'path':str(visual_path),'sha256':digest(visual_path),'viewports':[{'viewport':c['viewport'],'fonts':c['publication']['fonts'],'chart':c['publication']['chart'],'width':c['width'],'content_width':c['publication']['content_width'],'tooltip_font':c['tooltip']['font'],'long_subject':c['tree']} for c in visual['checks']]},'manual_visual_review':{'status':'passed','images':['visual-production/mobile-tooltip.png','visual-production/desktop-score.png','visual-candidate-r2/mobile-page.png','visual-candidate-r2/mobile-tree.png','visual-candidate-r2/desktop-page.png']},'temporary_8901_stopped':True,'reference_8900_preserved':True,'rollback_needed':False,'scope':'typography, spacing, localized display labels; existing statistical data and Go engine unchanged'}
(out/'final-acceptance.json').write_text(json.dumps(record,indent=2)+'\n')
print(json.dumps({'status':'passed','pid':current['pid'],'final_acceptance_sha256':digest(out/'final-acceptance.json')}))
