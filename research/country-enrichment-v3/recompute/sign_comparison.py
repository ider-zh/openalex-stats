"""Compare versions with one diagnostic coupled sign per temporal model chain.

Does not rewrite model artifacts. Uses earliest common-year country correlation;
the same factor applies to all years and both ECI and PCI, preserving annual
alignment. Raw rankings remain in the separate immutable comparison report.
"""
import argparse
import csv
import hashlib
import json
import math
from collections import defaultdict
from pathlib import Path

import duckdb


def corr(x, y):
    if len(x) < 3:
        return None
    mx, my = math.fsum(x)/len(x), math.fsum(y)/len(y)
    a = [v-mx for v in x]
    b = [v-my for v in y]
    den = math.sqrt(math.fsum(v*v for v in a)*math.fsum(v*v for v in b))
    return math.fsum(v*w for v,w in zip(a,b))/den if den else None


def read(c, path):
    cur = c.execute("select * from read_parquet(?) where valid", [str(path)])
    names = [r[0] for r in cur.description]
    groups = defaultdict(dict)
    for row in cur.fetchall():
        r = dict(zip(names,row))
        key = (r['scenario_id'],r['scheme'],r['level'],r['metric'])
        groups[(key,r['year'])][r['id']] = r
    return groups


def ranks(rows, factor=1):
    ordered = sorted(rows, key=lambda k: -factor*rows[k]['score'])
    out = {}
    prev = None
    rank = 0
    for i,k in enumerate(ordered):
        value = factor*rows[k]['score']
        if value != prev:
            rank = i+1
        out[k] = rank
        prev = value
    return out


def main():
    ap = argparse.ArgumentParser()
    for x in ('base','new','base-methods','new-methods','out'):
        ap.add_argument('--'+x, required=True)
    a = ap.parse_args()
    out = Path(a.out)
    out.mkdir(parents=True,exist_ok=False)
    c = duckdb.connect()
    report = {'diagnostic_only':True,'formal_model_files_modified':False,'methods':{}}
    for method, bp, np in [('spectral',a.base,a.new),('java',a.base_methods,a.new_methods)]:
        cb, cn = [read(c,Path(p)/'country_eci.parquet') for p in (bp,np)]
        factors = {}
        chains = []
        for key, year in sorted(set(cb)&set(cn),key=lambda t:t[1]):
            if key in factors:
                continue
            ids = sorted(set(cb[key,year]) & set(cn[key,year]))
            cc = corr([cb[key,year][i]['score'] for i in ids],[cn[key,year][i]['score'] for i in ids])
            if cc is None:
                continue
            factors[key] = -1 if cc < 0 else 1
            chains.append(dict(scenario_id=key[0],scheme=key[1],level=key[2],metric=key[3],anchor_year=year,common_countries=len(ids),raw_country_correlation=cc,multiplier=factors[key],weak_cross_version_correspondence=abs(cc)<.8))
        sums = {}
        for kind,filename in [('country','country_eci.parquet'),('discipline','discipline_pci.parquet')]:
            b,n = (cb,cn) if kind=='country' else [read(c,Path(p)/filename) for p in (bp,np)]
            output = []
            year_correlations = []
            for key,year in sorted(set(b)|set(n)):
                bb,nn = b.get((key,year),{}),n.get((key,year),{})
                factor = factors.get(key)
                if factor is None:
                    continue
                rb,rn,rraw = ranks(bb),ranks(nn,factor),ranks(nn)
                ids = sorted(set(bb)&set(nn))
                cc = corr([bb[i]['score'] for i in ids],[nn[i]['score'] for i in ids])
                year_correlations.append(dict(scheme=key[1],level=key[2],metric=key[3],year=year,common=len(ids),raw_corr=cc,matched_corr=cc*factor if cc is not None else None))
                for i in sorted(set(bb)|set(nn)):
                    br,nr = bb.get(i),nn.get(i)
                    output.append(dict(scenario_id=key[0],scheme=key[1],level=key[2],metric=key[3],year=year,id=i,diagnostic_multiplier=factor,before=br['score'] if br else None,raw_after=nr['score'] if nr else None,matched_after=factor*nr['score'] if nr else None,before_rank=rb.get(i),raw_after_rank=rraw.get(i),matched_after_rank=rn.get(i),matched_rank_improved=rb[i]-rn[i] if i in rb and i in rn else None,before_unstable=br['unstable'] if br else None,after_unstable=nr['unstable'] if nr else None))
            with (out/f'{method}-{kind}-signmatched-diagnostic.csv').open('w',newline='') as f:
                w=csv.DictWriter(f,fieldnames=list(output[0]));w.writeheader();w.writerows(output)
            valid=[r for r in output if r['matched_rank_improved'] is not None]
            sums[kind]=dict(rows=len(output),validity_set_changed=len(output)-len(valid),max_abs_matched_score_delta=max(abs(r['matched_after']-r['before']) for r in valid),max_matched_rank_change=max(abs(r['matched_rank_improved']) for r in valid),year_correlations=year_correlations,examples_2025=[r for r in valid if r['year']==2025 and r['scheme']=='concepts' and r['level']=='0' and r['metric']=='citations' and r['id'] in ('CN','US','SG','KP','DE')])
        report['methods'][method]={'chain_orientations':chains,'comparisons':sums}
    report['interpretation']='One earliest-year country factor orients both ECI and PCI for version-diff diagnostics only. Weak correlations or changed valid sets need separate interpretation; same sign is not proof of model stability. Existing unstable flags do not measure sign-anchor robustness.'
    (out/'review.json').write_text(json.dumps(report,indent=2)+'\n')
    hashes={p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in out.iterdir() if p.is_file()}
    (out/'sha256.json').write_text(json.dumps(hashes,indent=2)+'\n')
    print(json.dumps({'out':str(out),'chains':{k:v['chain_orientations'] for k,v in report['methods'].items()}}))


if __name__=='__main__':
    main()
