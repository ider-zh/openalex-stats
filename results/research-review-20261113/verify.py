"""Verify archived research bytes; optionally unpack into a separate empty folder."""
import argparse
import gzip
import hashlib
import json
from pathlib import Path

parser = argparse.ArgumentParser()
parser.add_argument('--extract', type=Path)
args = parser.parse_args()
base = Path(__file__).resolve().parent
manifest = json.loads((base / 'manifest.json').read_text(encoding='utf8'))
if args.extract:
    args.extract.mkdir(parents=True, exist_ok=False)
for entry in manifest['files']:
    source = base / entry['path']
    assert source.resolve().is_relative_to(base)
    raw = source.read_bytes()
    assert len(raw) == entry['bytes'], entry['path']
    assert hashlib.sha256(raw).hexdigest() == entry['sha256'], entry['path']
    unpacked = gzip.decompress(raw) if source.suffix == '.gz' else raw
    assert hashlib.sha256(unpacked).hexdigest() == entry['uncompressed_sha256'], entry['path']
    if args.extract:
        relative = Path(entry['path'])
        if relative.suffix == '.gz':
            relative = relative.with_suffix('')
        output = args.extract / relative
        assert output.resolve().is_relative_to(args.extract.resolve())
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_bytes(unpacked)
print(f"Verified {len(manifest['files'])} archived files; production unchanged.")
