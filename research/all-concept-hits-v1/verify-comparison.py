"""Summarize and assert the complete core/all count comparison outputs."""
import json
import pathlib

root = pathlib.Path('/home/ider/tmpdata/openalex-stats-dev-20261112/all-concept-hits-v1')
monotonic = json.loads((root / 'core-full-monotonicity.json').read_text())
integers = json.loads((root / 'full-integrality.json').read_text())
assert len(monotonic) == 24
assert len(integers) == 4
assert len({(r['scenario_id'], r['scheme'], r['level']) for r in monotonic}) == 24
bad = ['core_cells_missing_in_full', 'invalid_full_count_cells', 'publication_decreases', 'citation_decreases']
assert all(row[key] == 0 for row in monotonic for key in bad), monotonic
assert all(row[key] == 0 for row in integers for key in ['invalid_publication_integer_cells', 'invalid_citation_integer_cells']), integers
summary = {'status': 'passed', 'core_cells_compared': sum(r['core_cells'] for r in monotonic),
           'scenario_layer_groups': len(monotonic), 'error_totals': {key: sum(r[key] for r in monotonic) for key in bad},
           'whole_country_integer_cells': sum(r['cells'] for r in integers),
           'whole_country_invalid_publication_cells': sum(r['invalid_publication_integer_cells'] for r in integers),
           'whole_country_invalid_citation_cells': sum(r['invalid_citation_integer_cells'] for r in integers),
           'count_tolerance': '1e-7 + 1e-10 * abs(core)',
           'scope': 'All core country/year/discipline/scenario cells; count monotonicity only, not shares/RCA/ECI.'}
(root / 'comparison-summary.json').write_text(json.dumps(summary, indent=2) + '\n')
print(json.dumps(summary))
