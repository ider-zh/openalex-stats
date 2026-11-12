from html.parser import HTMLParser
from urllib.parse import urljoin,urlsplit
from pathlib import Path
import json,re
b=Path('/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112');v=b/'validation';req=json.load(open(v/'crossref-additional-primary-paper-evidence-requests.json'));wanted={r['doi'] for r in req['records']};first=json.load(open(v/'crossref-primary-publisher-html/manifest.json'));result={}
class Links(HTMLParser):
 def __init__(self):super().__init__();self.links=[]
 def handle_starttag(self,tag,attrs):
  a=dict(attrs)
  if tag=='meta' and a.get('name','').lower() in ['citation_pdf_url','citation_xml_url']:self.links.append(a.get('content',''))
  if tag=='a' and re.search(r'citation|format=(?:ris|bibtex|xml)|(?:\.pdf|\.xml|/pdf|/xml|/epdf)(?:\?|$)',a.get('href',''),re.I):self.links.append(a['href'])
for r in first['records']:
 if r['doi'] not in wanted:continue
 base=r.get('final_url','');links=[]
 if r.get('http_status')==200 and r.get('path') and Path(r['path']).exists() and base:
  p=Links();p.feed(Path(r['path']).read_text(errors='replace'))
  for x in p.links:
   u=urljoin(base,x);s=urlsplit(u)
   if s.scheme in ['https','http'] and s.hostname==urlsplit(base).hostname and not re.search(r'login|signin|purchase|paywall',u,re.I) and u!=base:links.append(u)
 result[r['doi']]=dict(final_url=base,http_status=r.get('http_status'),observed_public_representation_links=list(dict.fromkeys(links)))
(b/'metadata/crossref-observed-primary-links.json').write_text(json.dumps(result,indent=2)+'\n');print('works',len(result),'withlinks',sum(bool(r['observed_public_representation_links']) for r in result.values()))
