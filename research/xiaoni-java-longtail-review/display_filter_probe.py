"""Illustrate legacy post-score filtering on a frozen current matrix.

The thresholds below are an example, NOT recovered historical Java settings.
This neither recalculates RCA/ECI nor changes website output.
"""
import gzip
import hashlib
import json
from pathlib import Path

root = Path(__file__).resolve().parents[2]
source = root / 'results/research-review-20261113/complexity/representative-experiment-inputs.json.gz'
raw = gzip.decompress(source.read_bytes())
inputs = json.loads(raw)
item = next(x for x in inputs if x['corpus'] == 'Core' and x['year'] == 2025
            and x['scheme'] == 'concepts' and str(x['level']) == '0'
            and x['metric'] == 'citations')
result = item['baseline_java']
rows = sorted([{'country': result['countries'][i], 'score': score,
                'kc0': sum(result['advantage'][i])}
               for i, score in zip(result['active_countries'], result['eci'])],
              key=lambda x: (-x['score'], x['country']))
accepted, removed = [], []
for row in rows:
    # FzxByCtry.java 9875-9885, example parameters 0 / 3 / 3.
    if row['kc0'] <= 0 or (len(accepted) < 100 and row['kc0'] < 3):
        removed.append(row)
    else:
        accepted.append(row)
report = {
    'scope': 'Illustration only; not historical production settings or a new model',
    'input_uncompressed_sha256': hashlib.sha256(raw).hexdigest(),
    'corpus': 'Core', 'year': 2025, 'scheme': 'concepts', 'level': '0',
    'metric': 'citations', 'method': 'java-reflections-v1',
    'example_parameters': {'kc0_filter': 0, 'kc0_top100': 3, 'kc0_top100_2000': 3},
    'original_count': len(rows), 'remaining_count': len(accepted),
    'original_top12': rows[:12], 'filtered_top12': accepted[:12], 'removed': removed,
    'recomputed_network_or_scores': False,
    'interpretation': 'AG/EH disappear, but SX/MQ/PR lead instead. This is output selection, not evidence that the remaining axis measures national capability. Tie order here is code order; Java input order is not established.',
}
output = Path(__file__).with_name('display-filter-illustration.json')
output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n', encoding='utf8')
print(json.dumps({'output': str(output), 'removed': [r['country'] for r in removed], 'remaining': len(accepted)}))
