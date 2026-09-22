"""Local Qdrant provisioning and individually revocable Hive credentials.

Run on the Qdrant host as its operator, never as the Hive process. Secrets are
read from private files; credentials are written only to newly created files.
TLS certificate validation is mandatory. Uses only Python's standard library.
"""

import argparse
import base64
import hashlib
import hmac
import ipaddress
import json
import os
from pathlib import Path
import re
import secrets
import ssl
import stat
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid


class AdminError(Exception):
    pass


def identifier(value, maximum=64):
    if not re.fullmatch(r"[a-z0-9][a-z0-9_-]{0," + str(maximum - 1) + r"}", value):
        raise argparse.ArgumentTypeError("invalid lowercase identifier")
    return value


def write_private(path, content):
    path = Path(path)
    # Exclusive creation prevents accidental token/key replacement or symlinks.
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8") as output:
        output.write(content)
        output.flush()
        os.fsync(output.fileno())


def read_key(path):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd, "r", encoding="utf-8") as source:
        metadata = os.fstat(source.fileno())
        if not stat.S_ISREG(metadata.st_mode) or metadata.st_mode & 0o077:
            raise AdminError("admin key must be a regular private file (0600)")
        key = source.read(8193).strip()
    if len(key) < 32 or len(key) > 8192 or any(ord(c) < 33 for c in key):
        raise AdminError("invalid admin key file")
    return key


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        raise AdminError("Qdrant redirect rejected")


class Qdrant:
    def __init__(self, url, key, ca_file):
        parsed = urllib.parse.urlsplit(url)
        try:
            local = ipaddress.ip_address(parsed.hostname or "").is_loopback
        except ValueError:
            local = parsed.hostname == "localhost"
        if (parsed.scheme != "https" or not local or parsed.username or
                parsed.password or parsed.path not in ("", "/") or
                parsed.query or parsed.fragment):
            raise AdminError("administration requires a loopback HTTPS REST endpoint")
        self.url, self.key = url.rstrip("/"), key
        tls = ssl.create_default_context(cafile=ca_file)
        tls.minimum_version = ssl.TLSVersion.TLSv1_2
        self.opener = urllib.request.build_opener(
            urllib.request.ProxyHandler({}), NoRedirect(),
            urllib.request.HTTPSHandler(context=tls))

    def request(self, method, path, body=None, allow_missing=False):
        data = None if body is None else json.dumps(body).encode()
        request = urllib.request.Request(self.url + path, data=data, method=method,
                                        headers={"api-key": self.key,
                                                 "Content-Type": "application/json"})
        try:
            with self.opener.open(request, timeout=60) as response:
                result = json.loads(response.read(4 << 20))
        except urllib.error.HTTPError as error:
            code = error.code
            error.close()
            if allow_missing and code == 404:
                return None
            raise AdminError("Qdrant rejected administrative request (HTTP %d)" % code) from None
        if result.get("status") != "ok":
            raise AdminError("Qdrant did not confirm administrative request")
        return result["result"]


def ensure_collection(client, name, schema):
    path = "/collections/" + name
    current = client.request("GET", path, allow_missing=True)
    if current is None:
        client.request("PUT", path, schema)
        current = client.request("GET", path)
    params = current["config"]["params"]
    vectors = params.get("vectors", {})
    expected = schema["vectors"]
    if expected:
        if vectors.get("size") != expected["size"] or vectors.get("distance") != "Cosine":
            raise AdminError("existing data collection has an incompatible dense vector")
        if "sparse" not in (params.get("sparse_vectors") or {}):
            raise AdminError("existing data collection lacks the sparse vector")
    elif vectors:
        raise AdminError("administrative/control collection must be vectorless")


def provision(client, collection, access_collection, dimension):
    if not 1 <= dimension <= 65536:
        raise AdminError("dimension must be between 1 and 65536")
    ensure_collection(client, collection, {
        "vectors": {"size": dimension, "distance": "Cosine"},
        "sparse_vectors": {"sparse": {}}})
    ensure_collection(client, collection + "__control", {"vectors": {}})
    ensure_collection(client, access_collection, {"vectors": {}})
    for field in ("device_id", "token_id"):
        client.request("PUT", "/collections/" + access_collection + "/index?wait=true",
                       {"field_name": field, "field_schema": "keyword"})


def health(client):
    client.request("GET", "/collections")
    return {"healthy": True}


def encode_jwt(key, claims):
    def encoded(value):
        raw = json.dumps(value, separators=(",", ":")).encode()
        return base64.urlsafe_b64encode(raw).rstrip(b"=")
    message = encoded({"alg": "HS256", "typ": "JWT"}) + b"." + encoded(claims)
    signature = hmac.new(key.encode(), message, hashlib.sha256).digest()
    return (message + b"." + base64.urlsafe_b64encode(signature).rstrip(b"=")).decode()


def issue(client, collection, access_collection, device, role, hours, output):
    if not 1 <= hours <= 168:
        raise AdminError("token lifetime must be between 1 and 168 hours")
    if Path(output).exists() or Path(output).is_symlink():
        raise AdminError("token output already exists; choose a new file")
    token_id, now = str(uuid.uuid4()), int(time.time())
    expires = now + hours * 3600
    grant = "rw" if role == "writer" else "r"
    claims = {
        "sub": device, "jti": token_id, "iat": now, "exp": expires,
        "access": [{"collection": name, "access": grant}
                   for name in (collection, collection + "__control")],
        "value_exists": {"collection": access_collection, "matches": [
            {"key": "device_id", "value": device},
            {"key": "token_id", "value": token_id}]}}
    client.request("PUT", "/collections/" + access_collection + "/points?wait=true", {
        "points": [{"id": token_id, "vector": {}, "payload": {
            "device_id": device, "token_id": token_id, "role": role,
            "collection": collection, "expires_at": expires}}]})
    write_private(output, encode_jwt(client.key, claims) + "\n")
    return {"device_id": device, "token_id": token_id, "expires_at": expires}


def revoke(client, access_collection, device, token_id=None):
    conditions = [{"key": "device_id", "match": {"value": device}}]
    if token_id:
        conditions.append({"key": "token_id", "match": {"value": str(uuid.UUID(token_id))}})
    client.request("POST", "/collections/" + access_collection + "/points/delete?wait=true",
                   {"filter": {"must": conditions}})


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--url", default="https://127.0.0.1:6333")
    parser.add_argument("--key-file", default="/srv/hive-private/admin.key")
    parser.add_argument("--ca-file", default="/srv/hive-private/ca.crt")
    parser.add_argument("--collection", default="hive_mind_v01", type=lambda v: identifier(v, 55))
    parser.add_argument("--access-collection", default="hive_device_access", type=identifier)
    sub = parser.add_subparsers(dest="operation", required=True)
    initialize = sub.add_parser("init", help="generate new operator secrets; refuses replacement")
    initialize.add_argument("--directory", default="/srv/hive-private")
    create = sub.add_parser("provision", help="create or validate the three collections")
    create.add_argument("--dimension", type=int, required=True)
    sub.add_parser("health", help="verify authenticated TLS access to Qdrant")
    token = sub.add_parser("issue", help="issue one credential to a private new file")
    token.add_argument("--device-id", type=identifier, required=True)
    token.add_argument("--role", choices=("writer", "reader"), required=True)
    token.add_argument("--hours", type=int, default=8)
    token.add_argument("--output", required=True)
    remove = sub.add_parser("revoke", help="revoke device credentials (optionally one token)")
    remove.add_argument("--device-id", type=identifier, required=True)
    remove.add_argument("--token-id")
    args = parser.parse_args()
    os.umask(0o077)
    try:
        if args.operation == "init":
            root = Path(args.directory)
            if not root.is_absolute() or root.is_symlink():
                raise AdminError("absolute private directory required")
            root.mkdir(mode=0o700, parents=True, exist_ok=True)
            if root.stat().st_mode & 0o077:
                raise AdminError("operator directory must have mode 0700")
            if (root / "admin.key").exists() or (root / "qdrant.env").exists():
                raise AdminError("operator secrets already exist; initialization stopped")
            key = secrets.token_hex(32)
            write_private(root / "admin.key", key + "\n")
            write_private(root / "qdrant.env", "QDRANT__SERVICE__API_KEY=" + key + "\n")
            result = {"initialized": True}
        else:
            if args.access_collection in (args.collection, args.collection + "__control"):
                raise AdminError("revocation collection must be separate from Hive collections")
            client = Qdrant(args.url, read_key(args.key_file), args.ca_file)
            if args.operation == "health":
                result = health(client)
            elif args.operation == "provision":
                provision(client, args.collection, args.access_collection, args.dimension)
                result = {"provisioned": True}
            elif args.operation == "issue":
                result = issue(client, args.collection, args.access_collection,
                               args.device_id, args.role, args.hours, args.output)
            else:
                revoke(client, args.access_collection, args.device_id, args.token_id)
                result = {"revoked": True, "device_id": args.device_id}
        print(json.dumps(dict(ok=True, **result)))
    except AdminError as error:
        print(json.dumps({"ok": False, "error": str(error)}))
        raise SystemExit(1)
    except Exception:
        print(json.dumps({"ok": False, "error": "administration failed; check files, TLS and service availability"}))
        raise SystemExit(1)


if __name__ == "__main__":
    main()
