"""Publish checksummed research artifacts using the existing Git credential helper.

Credentials stay in memory and are sent only to api.github.com / uploads.github.com.
The GitHub connector has no release-asset upload tool; native Git handles commits.
"""
import argparse
import hashlib
import http.client
import json
import pathlib
import re
import subprocess
import sys
import time
import urllib.parse


def credential():
    result = subprocess.run(
        ["git", "-c", "credential.interactive=never", "credential", "fill"],
        input="protocol=https\nhost=github.com\n\n", text=True,
        capture_output=True, check=False,
    )
    values = dict(line.split("=", 1) for line in result.stdout.splitlines() if "=" in line)
    if result.returncode or not values.get("password"):
        raise RuntimeError("GitHub credential helper has no noninteractive credential")
    return values["password"]


def request(token, method, url, payload=None, source=None):
    parsed = urllib.parse.urlsplit(url)
    if parsed.scheme != "https" or parsed.hostname not in {"api.github.com", "uploads.github.com"} or parsed.username or parsed.port:
        raise RuntimeError("Refused unexpected GitHub API destination")
    connection = http.client.HTTPSConnection(parsed.hostname, timeout=900)
    target = parsed.path + ("?" + parsed.query if parsed.query else "")
    body = json.dumps(payload).encode() if payload is not None else b""
    connection.putrequest(method, target)
    connection.putheader("Authorization", "Bearer " + token)
    connection.putheader("Accept", "application/vnd.github+json")
    connection.putheader("X-GitHub-Api-Version", "2022-11-28")
    connection.putheader("User-Agent", "openalex-stats-artifact-publisher")
    if source:
        connection.putheader("Content-Type", "application/octet-stream")
        connection.putheader("Content-Length", str(source.stat().st_size))
    elif payload is not None:
        connection.putheader("Content-Type", "application/json")
        connection.putheader("Content-Length", str(len(body)))
    connection.endheaders()
    if source:
        with source.open("rb") as stream:
            while chunk := stream.read(1024 * 1024):
                connection.send(chunk)
    elif body:
        connection.send(body)
    response = connection.getresponse()
    raw = response.read()
    status = response.status
    connection.close()
    try:
        value = json.loads(raw)
    except json.JSONDecodeError:
        value = {"message": "non-JSON GitHub response"}
    return status, value


def digest(path):
    checksum = hashlib.sha256()
    with path.open("rb") as stream:
        while chunk := stream.read(4 * 1024 * 1024):
            checksum.update(chunk)
    return checksum.hexdigest()


def publish(args):
    if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", args.repo):
        raise RuntimeError("Invalid repository name")
    token = credential()
    base = f"https://api.github.com/repos/{args.repo}"
    status, release = request(token, "GET", base + "/releases/tags/" + urllib.parse.quote(args.tag, safe=""))
    if status == 404:
        status, release = request(token, "POST", base + "/releases", {
            "tag_name": args.tag, "target_commitish": args.ref,
            "name": "OpenAlex 2026-06-26 — audited core and core+XPAC statistics",
            "body": "Immutable audited results, checksums and methodology sidecars. The repository contains the complete Cloudflare query dataset and source. Country-enrichment candidates are excluded until review.",
            "draft": False, "prerelease": False,
        })
    if status not in (200, 201):
        raise RuntimeError(f"GitHub release API returned {status}: {release.get('message', '')}")
    entries = json.loads(pathlib.Path(args.manifest).read_text(encoding="utf-8-sig"))
    assets = {asset["name"]: asset for asset in release.get("assets", [])}
    upload = release["upload_url"].split("{", 1)[0]
    report = {"repository": args.repo, "tag": args.tag, "release_url": release["html_url"], "assets": []}
    for entry in entries:
        name = entry["name"]
        if not re.fullmatch(r"[A-Za-z0-9_.-]+", name):
            raise RuntimeError("Invalid release asset name")
        path = pathlib.Path(entry["source"])
        expected = entry.get("sha256") or digest(path)
        size = path.stat().st_size
        if size >= 2 * 1024 * 1024 * 1024:
            raise RuntimeError(f"Release asset exceeds 2 GiB: {name}")
        if entry.get("bytes") is not None and entry["bytes"] != size:
            raise RuntimeError(f"Local artifact size mismatch: {name}")
        if digest(path) != expected:
            raise RuntimeError(f"Local artifact checksum mismatch: {name}")
        existing = assets.get(name)
        if existing:
            if existing.get("size") != size or existing.get("digest") != "sha256:" + expected or existing.get("state") != "uploaded":
                raise RuntimeError(f"Existing immutable release asset differs: {name}")
            asset = existing
            print(json.dumps({"asset": name, "status": "verified_existing", "bytes": size}), flush=True)
        else:
            started = time.monotonic()
            print(json.dumps({"asset": name, "status": "uploading", "bytes": size}), flush=True)
            status, asset = request(token, "POST", upload + "?name=" + urllib.parse.quote(name, safe=""), source=path)
            if status != 201:
                raise RuntimeError(f"GitHub asset upload returned {status}: {asset.get('message', '')}")
            if asset.get("size") != size or asset.get("digest") != "sha256:" + expected:
                raise RuntimeError(f"GitHub uploaded asset digest mismatch: {name}")
            print(json.dumps({"asset": name, "status": "uploaded_verified", "seconds": round(time.monotonic()-started, 2)}), flush=True)
        report["assets"].append({"name": name, "bytes": size, "sha256": expected, "url": asset["browser_download_url"], "id": asset["id"]})
        pathlib.Path(args.output).write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({"status": "passed", "release": report["release_url"], "assets": len(report["assets"]), "bytes": sum(a["bytes"] for a in report["assets"])}))


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo", default="ider-zh/openalex-stats")
    parser.add_argument("--tag", default="snapshot-20260626-v1")
    parser.add_argument("--ref", default="main")
    parser.add_argument("--manifest", required=True, help="JSON array of local source/name/bytes/sha256 entries")
    parser.add_argument("--output", required=True, help="verified public release index JSON")
    try:
        publish(parser.parse_args())
    except Exception as error:
        print("Artifact publisher: " + str(error), file=sys.stderr)
        sys.exit(1)
