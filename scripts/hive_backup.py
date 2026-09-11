"""Paired Qdrant backups encrypted/authenticated by an operator-owned restic repo.

The writer must be stopped for the whole backup. Restoration only writes a new
private staging directory; loading collections into an isolated Qdrant is a
separate operator action described in the recovery runbook.
"""
import argparse
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
import ssl
import subprocess
import tempfile
import time
import urllib.parse
import urllib.request
import uuid


class BackupError(Exception):
    pass


def identifier(value, maximum=64):
    if not re.fullmatch(r"[a-z0-9][a-z0-9_-]{0," + str(maximum - 1) + r"}", value):
        raise BackupError("invalid backup identity")
    return value


def private_directory(value):
    path = Path(value)
    if not path.is_absolute() or not path.is_dir() or path.is_symlink():
        raise BackupError("private staging/audit directory required")
    if path.stat().st_mode & 0o077:
        raise BackupError("staging/audit directory must have mode 0700")
    return path.resolve()


def service_url(value):
    url = urllib.parse.urlsplit(value)
    if url.scheme not in ("http", "https") or not url.hostname or url.username or url.password or url.query or url.fragment or url.path not in ("", "/"):
        raise BackupError("invalid Qdrant REST endpoint")
    try:
        local = ipaddress.ip_address(url.hostname).is_loopback
    except ValueError:
        local = url.hostname == "localhost"
    if not local and url.scheme != "https":
        raise BackupError("remote backup requires TLS")
    return value.rstrip("/")


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        raise BackupError("Qdrant redirect rejected")


class Qdrant:
    def __init__(self, env):
        self.url = service_url(env["HIVE_BACKUP_QDRANT_URL"])
        self.key = env["QDRANT_API_KEY"]
        if not self.key or any(ord(c) < 32 for c in self.key):
            raise BackupError("backup credential required")
        tls = ssl.create_default_context(cafile=env.get("QDRANT_TLS_CA_FILE") or None)
        tls.minimum_version = ssl.TLSVersion.TLSv1_2
        # Do not send credentials through inherited HTTP proxies or redirects.
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect(), urllib.request.HTTPSHandler(context=tls))

    def open(self, path, method="GET"):
        request = urllib.request.Request(self.url + path, method=method, headers={"api-key": self.key})
        return self.opener.open(request, timeout=60)

    def snapshot(self, collection, destination):
        with self.open("/collections/" + collection + "/snapshots", "POST") as response:
            metadata = json.loads(response.read(1 << 20))
        name = metadata["result"]["name"]
        if not isinstance(name, str) or not re.fullmatch(r"[a-zA-Z0-9_.-]+", name) or name in (".", ".."):
            raise BackupError("invalid snapshot response")
        with self.open("/collections/" + collection + "/snapshots/" + name) as response, destination.open("xb") as output:
            size = 0
            while block := response.read(1 << 20):
                size += len(block)
                if size > 10 << 30:
                    raise BackupError("snapshot exceeds 10 GiB limit")
                output.write(block)
            output.flush()
            os.fsync(output.fileno())
        if size == 0:
            raise BackupError("empty snapshot")


def digest(path):
    result = hashlib.sha256()
    with path.open("rb") as source:
        while block := source.read(1 << 20):
            result.update(block)
    return result.hexdigest()


def restic(*args, cwd=None):
    # Restic reads repository/password/backend credentials from the environment.
    # Never print subprocess output, which includes local paths and backend data.
    result = subprocess.run(["restic", *args], cwd=cwd, capture_output=True, timeout=3600, check=False)
    if result.returncode:
        raise BackupError("restic operation failed")
    return result.stdout


def save_json(path, value):
    with path.open("x", encoding="utf-8") as output:
        json.dump(value, output, sort_keys=True)
        output.write("\n")
        output.flush()
        os.fsync(output.fileno())


def verify_pair(root, hive, collection):
    manifest_path = root / "manifest.json"
    if manifest_path.is_symlink() or manifest_path.stat().st_size > 1 << 20:
        raise BackupError("invalid restore manifest")
    manifest = json.loads(manifest_path.read_text())
    if manifest.get("schema_version") != 1 or manifest.get("hive_id") != hive or manifest.get("collection") != collection:
        raise BackupError("restored Hive identity mismatch")
    files = manifest.get("files", {})
    if set(files) != {"data.snapshot", "control.snapshot"}:
        raise BackupError("backup must contain both collections")
    for name, expected in files.items():
        path = root / name
        if path.is_symlink() or not path.is_file() or digest(path) != expected:
            raise BackupError("restored snapshot integrity mismatch")
    return manifest


def backup(env, staging, hive, collection):
    if env.get("HIVE_BACKUP_WRITER_STOPPED") != "yes":
        raise BackupError("stop the writer and confirm HIVE_BACKUP_WRITER_STOPPED=yes")
    qdrant = Qdrant(env)
    with tempfile.TemporaryDirectory(prefix="hive-backup-", dir=staging) as temporary:
        root = Path(temporary)
        qdrant.snapshot(collection, root / "data.snapshot")
        qdrant.snapshot(collection + "__control", root / "control.snapshot")
        manifest = {"schema_version": 1, "hive_id": hive, "collection": collection,
                    "created_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
                    "files": {name: digest(root / name) for name in ("data.snapshot", "control.snapshot")}}
        save_json(root / "manifest.json", manifest)
        output = restic("backup", "--json", "--tag", "hive=" + hive, ".", cwd=root)
        snapshot_id = None
        for line in output.splitlines():
            record = json.loads(line)
            if record.get("message_type") == "summary":
                snapshot_id = record.get("snapshot_id")
        if not isinstance(snapshot_id, str) or not re.fullmatch(r"[a-f0-9]{8,64}", snapshot_id):
            raise BackupError("restic did not confirm a snapshot")
        restic("check", "--read-data")
        return {"snapshot_id": snapshot_id, "pair_verified": True}


def restore(env, staging, hive, collection, snapshot):
    if not re.fullmatch(r"[a-f0-9]{8,64}", snapshot or ""):
        raise BackupError("explicit restic snapshot id required")
    target = staging / ("hive-restore-" + uuid.uuid4().hex)
    target.mkdir(mode=0o700)
    restic("check", "--read-data")
    restic("restore", snapshot, "--target", str(target))
    verify_pair(target, hive, collection)
    return {"snapshot_id": snapshot, "staging_directory": target.name, "pair_verified": True,
            "qdrant_restore_drill": "pending"}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("operation", choices=("backup", "restore"))
    parser.add_argument("--snapshot")
    args = parser.parse_args()
    os.umask(0o077)
    try:
        env = os.environ
        hive = identifier(env["HIVE_ID"])
        collection = identifier(env["HIVE_COLLECTION"], 55)
        staging = private_directory(env["HIVE_BACKUP_STAGING_DIR"])
        audit = private_directory(env["HIVE_AUDIT_DIR"])
        if not env.get("RESTIC_REPOSITORY") or not (env.get("RESTIC_PASSWORD") or env.get("RESTIC_PASSWORD_FILE")):
            raise BackupError("encrypted restic repository credentials required")
        event = {"action": args.operation, "hive_id": hive, "operation_id": uuid.uuid4().hex,
                 "time": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())}
        save_json(audit / ("backup-" + event["operation_id"] + "-prepared.json"), dict(event, outcome="prepared"))
        result = backup(env, staging, hive, collection) if args.operation == "backup" else restore(env, staging, hive, collection, args.snapshot)
        save_json(audit / ("backup-" + event["operation_id"] + "-completed.json"), dict(event, outcome="completed", **result))
        print(json.dumps(dict(ok=True, **result)))
    except Exception:
        # No provider messages, local paths, credentials or tracebacks.
        print(json.dumps({"ok": False, "error": "backup/restore failed; check configuration, audit storage, repository and service availability"}))
        raise SystemExit(15)


if __name__ == "__main__":
    main()
