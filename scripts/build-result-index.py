"""Build and verify the public result index from immutable local mirrors."""
import argparse
import hashlib
import json
import pathlib

RELEASE = "https://github.com/ider-zh/openalex-stats/releases/download/snapshot-20260626-v1"

def sha(path):
    h = hashlib.sha256()
    with path.open("rb") as stream:
        while part := stream.read(4 * 1024 * 1024): h.update(part)
    return h.hexdigest()

def main(args):
    root = pathlib.Path(args.mirror).resolve()
    assets, uploads, datasets = [], [], {}
    def add(corpus, group, folder, filename, expected=None):
        path = folder / filename
        actual_sha, size = sha(path), path.stat().st_size
        if expected and (expected["sha256"] != actual_sha or expected["bytes"] != size):
            raise RuntimeError(f"Completion mismatch: {corpus}/{group}/{filename}")
        prefix = (corpus or "") + ("-methods" if group == "methods" else "")
        name = prefix + "-" + filename if corpus else filename
        public = {"name": name, "filename": filename, "corpus": corpus, "group": group, "bytes": size, "sha256": actual_sha, "url": RELEASE + "/" + name}
        assets.append(public)
        uploads.append({"name": name, "source": str(path), "bytes": size, "sha256": actual_sha})
    for corpus in ("core", "all"):
        base = root / (corpus + "-concept-hits-v1")
        completion = json.loads((base / "completion.json").read_text())
        run = json.loads((base / "run.json").read_text())
        datasets[corpus] = {"run_id": completion["run_id"], "completion_sha256": sha(base / "completion.json"), "selected_works": run["selected_works"], "publication_works": run["quality"]["publication_works"], "retained_edges": run["graph"]["retained_edges"]}
        for entry in [completion["run"], *completion["artifacts"]]:
            add(corpus, "base", base, pathlib.Path(entry["path"]).name, entry)
        add(corpus, "base", base, "completion.json")
        methods = root / (corpus + "-java-reflections-v1")
        mc = json.loads((methods / "completion.json").read_text())
        manifest = json.loads((methods / "manifest.json").read_text())
        if mc["status"] != "complete" or mc["base_run_id"] != completion["run_id"] or manifest["source_completion_sha256"] != datasets[corpus]["completion_sha256"] or sha(methods / "manifest.json") != mc["manifest_sha256"]:
            raise RuntimeError("Method sidecar does not match base result")
        datasets[corpus]["methods"] = manifest["methods"]
        datasets[corpus]["method_completion_sha256"] = sha(methods / "completion.json")
        for entry in mc["artifacts"]: add(corpus, "methods", methods, pathlib.Path(entry["path"]).name, entry)
        add(corpus, "methods", methods, "completion.json")
    for filename in ("source-v9-compute.tar.gz", "source-methods-v1.tar.gz"):
        add(None, "provenance", pathlib.Path("bin").resolve(), filename)
    if len({a["name"] for a in assets}) != len(assets): raise RuntimeError("Duplicate public asset name")
    index = {"version": 1, "status": "prepared", "snapshot_date": "2026-06-26", "release_tag": "snapshot-20260626-v1", "release_url": "https://github.com/ider-zh/openalex-stats/releases/tag/snapshot-20260626-v1", "datasets": datasets, "assets": sorted(assets, key=lambda a: a["name"])}
    pathlib.Path(args.output).write_text(json.dumps(index, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    pathlib.Path(args.upload_manifest).write_text(json.dumps(uploads, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({"assets": len(assets), "bytes": sum(a["bytes"] for a in assets), "max_file_bytes": max(a["bytes"] for a in assets)}))

if __name__ == "__main__":
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--mirror", default="bin/raw-results")
    p.add_argument("--output", default="results/index.json")
    p.add_argument("--upload-manifest", default="bin/release-upload.json")
    main(p.parse_args())
