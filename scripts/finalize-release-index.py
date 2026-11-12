"""Mark the public result index published only after every remote digest matches."""
import argparse
import json
import pathlib
from datetime import datetime, timezone

p = argparse.ArgumentParser(description=__doc__)
p.add_argument("--index", default="results/index.json")
p.add_argument("--report", required=True)
a = p.parse_args()
index = json.loads(pathlib.Path(a.index).read_text(encoding="utf-8"))
report = json.loads(pathlib.Path(a.report).read_text(encoding="utf-8"))
remote = {entry["name"]: entry for entry in report["assets"]}
for entry in index["assets"]:
    found = remote.get(entry["name"])
    if not found or any(found[k] != entry[k] for k in ("bytes", "sha256", "url")):
        raise RuntimeError("Incomplete or mismatched GitHub Release: " + entry["name"])
    entry["github_asset_id"] = found["id"]
index["status"] = "published"
index["verified_at_utc"] = datetime.now(timezone.utc).isoformat()
pathlib.Path(a.index).write_text(json.dumps(index, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
print(json.dumps({"status": "published", "assets": len(index["assets"]), "release_url": index["release_url"]}))
