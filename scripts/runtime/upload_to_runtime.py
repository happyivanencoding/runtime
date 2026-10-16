#!/usr/bin/env python3
"""Upload a ZIP from any cloud machine to a local Runtime via its HTTPS tunnel.

Requires only the Python standard library and an upload-only Runtime token.
The sidecar .runtime-upload.json allows retries after interruption.
"""
import argparse
import json
import os
from pathlib import Path
import sys
import time
import urllib.error
import urllib.request


def request(base, token, method, path, payload=None, headers=None):
    h = {"Authorization": "Bearer " + token}
    if headers:
        h.update(headers)
    req = urllib.request.Request(base + path, data=payload, headers=h, method=method)
    try:
        with urllib.request.urlopen(req, timeout=180) as response:
            raw = response.read()
            return json.loads(raw) if raw else {}
    except urllib.error.HTTPError as exc:
        detail = exc.read(1000).decode("utf-8", errors="replace").strip()
        raise RuntimeError(f"{method} {path}: HTTP {exc.code}: {detail}") from exc


def upload(path, origin, token):
    stat = path.stat()
    if not path.is_file() or path.suffix.lower() != ".zip":
        raise ValueError("Provide an existing .zip file")
    if stat.st_size == 0:
        raise ValueError("Cannot upload an empty file")
    if origin.endswith("/mcp"):
        origin = origin[:-4]
    origin = origin.rstrip("/")
    if not (origin.startswith("https://") or origin.startswith("http://127.0.0.1:")):
        raise ValueError("Use HTTPS for the public Runtime endpoint")
    base_path = "/transfers/v1/uploads"
    sidecar = path.with_name(path.name + ".runtime-upload.json")
    identity = {
        "origin": origin,
        "filename": path.name,
        "size": stat.st_size,
        "mtime_ns": stat.st_mtime_ns,
    }

    if sidecar.exists():
        saved = json.loads(sidecar.read_text(encoding="utf-8"))
        if any(saved.get(key) != value for key, value in identity.items()):
            raise RuntimeError(f"ZIP changed since transfer began; move/delete {sidecar} to start anew")
        upload_id = saved["upload_id"]
    else:
        created = request(
            origin, token, "POST", base_path,
            json.dumps({"filename": path.name, "size": stat.st_size}).encode("utf-8"),
            {"Content-Type": "application/json"},
        )
        upload_id = created["upload_id"]
        sidecar.write_text(json.dumps({**identity, "upload_id": upload_id}), encoding="utf-8")

    item_path = base_path + "/" + upload_id
    attempts = 0
    with path.open("rb") as source:
        while True:
            remote = request(origin, token, "GET", item_path)
            if remote["complete"]:
                print("Already completed:", remote["path"])
                sidecar.unlink(missing_ok=True)
                return
            offset = remote["offset"]
            if offset == stat.st_size:
                break
            chunk_size = min(remote["chunk_size"], stat.st_size - offset)
            source.seek(offset)
            chunk = source.read(chunk_size)
            try:
                result = request(
                    origin, token, "PUT", item_path, chunk,
                    {"Content-Type": "application/octet-stream", "Upload-Offset": str(offset)},
                )
                if result["offset"] != offset + len(chunk):
                    raise RuntimeError("Server returned an unexpected upload offset")
                attempts = 0
                print(f"Uploaded {result['offset']:,} / {stat.st_size:,} bytes", flush=True)
            except RuntimeError as error:
                if "HTTP 401:" in str(error):
                    raise
                attempts += 1
                if attempts > 5:
                    raise RuntimeError("Upload repeatedly failed; resume by rerunning this command") from error
                print(f"Interrupted; checking saved offset ({attempts}/5): {error}", file=sys.stderr)
                time.sleep(min(2 * attempts, 10))

    result = request(origin, token, "POST", item_path + "/complete", b"")
    if not result["complete"] or result["size"] != stat.st_size:
        raise RuntimeError("Runtime did not confirm upload completion")
    sidecar.unlink(missing_ok=True)
    print("Success. Local path:", result["path"])


def main():
    parser = argparse.ArgumentParser(description="Transfer ZIP to your own Runtime Windows PC")
    parser.add_argument("zip_path", type=Path, help="ZIP file on this cloud machine")
    parser.add_argument("--url", default="https://runtime.thegreatnovel.com",
                        help="Runtime public origin or full /mcp endpoint")
    args = parser.parse_args()
    token = os.environ.get("RUNTIME_UPLOAD_TOKEN", "").strip()
    if not token:
        parser.error("Set RUNTIME_UPLOAD_TOKEN in this cloud computer's environment first")
    try:
        upload(args.zip_path.resolve(), args.url, token)
    except (OSError, RuntimeError, ValueError, urllib.error.URLError, KeyError) as error:
        print("Upload failed:", error, file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
