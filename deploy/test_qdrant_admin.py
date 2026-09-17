"""Offline checks for administrative privilege boundaries; no real credentials."""

import base64
import hashlib
import hmac
import json
from pathlib import Path
import tempfile
import unittest

import qdrant_admin as admin


class FakeQdrant:
    key = "synthetic-administration-key-for-tests"

    def __init__(self):
        self.calls = []
        self.collections = {}

    def request(self, method, path, body=None, allow_missing=False):
        self.calls.append((method, path, body))
        if method == "GET":
            return self.collections.get(path)
        if method == "PUT" and "/index" not in path and "/points" not in path:
            self.collections[path] = {"config": {"params": body}}
        return True


class AdministrationTests(unittest.TestCase):
    def test_token_has_only_pair_access_and_individual_revocation(self):
        for role, expected in (("writer", "rw"), ("reader", "r")):
            with self.subTest(role=role), tempfile.TemporaryDirectory() as directory:
                client = FakeQdrant()
                target = Path(directory) / "token"
                report = admin.issue(client, "hive_data", "device_access", "ana-laptop", role, 1, target)
                header, body, signature = target.read_text().strip().split(".")
                claims = json.loads(base64.urlsafe_b64decode(body + "=" * (-len(body) % 4)))
                self.assertEqual(claims["access"], [
                    {"collection": "hive_data", "access": expected},
                    {"collection": "hive_data__control", "access": expected}])
                self.assertEqual(claims["value_exists"]["collection"], "device_access")
                self.assertEqual(claims["value_exists"]["matches"], [
                    {"key": "device_id", "value": "ana-laptop"},
                    {"key": "token_id", "value": report["token_id"]}])
                calculated = hmac.new(client.key.encode(), (header + "." + body).encode(), hashlib.sha256).digest()
                self.assertEqual(base64.urlsafe_b64encode(calculated).rstrip(b"=").decode(), signature)
                self.assertEqual(target.stat().st_mode & 0o777, 0o600)
                self.assertNotIn("access", report)
                self.assertEqual(claims["exp"] - claims["iat"], 3600)
                point = client.calls[-1][2]["points"][0]
                self.assertEqual(point["payload"]["token_id"], claims["jti"])

    def test_existing_token_is_not_overwritten_or_granted_again(self):
        with tempfile.TemporaryDirectory() as directory:
            target = Path(directory) / "token"
            admin.write_private(target, "existing")
            client = FakeQdrant()
            with self.assertRaises(admin.AdminError):
                admin.issue(client, "hive_data", "device_access", "ana-laptop", "reader", 8, target)
            self.assertEqual(target.read_text(), "existing")
            self.assertEqual(client.calls, [])

    def test_provision_refuses_incompatible_collection_without_replacement(self):
        client = FakeQdrant()
        client.collections["/collections/hive_data"] = {"config": {"params": {
            "vectors": {"size": 3, "distance": "Cosine"}}}}
        with self.assertRaises(admin.AdminError):
            admin.provision(client, "hive_data", "device_access", 768)
        self.assertEqual([call[0] for call in client.calls], ["GET"])

    def test_provision_and_revoke_target_expected_collections(self):
        client = FakeQdrant()
        admin.provision(client, "hive_data", "device_access", 768)
        self.assertEqual(set(client.collections), {
            "/collections/hive_data", "/collections/hive_data__control", "/collections/device_access"})
        admin.revoke(client, "device_access", "ana-laptop")
        self.assertEqual(client.calls[-1], ("POST", "/collections/device_access/points/delete?wait=true", {
            "filter": {"must": [{"key": "device_id", "match": {"value": "ana-laptop"}}]}}))

    def test_key_requires_private_regular_file(self):
        with tempfile.TemporaryDirectory() as directory:
            target = Path(directory) / "admin.key"
            admin.write_private(target, FakeQdrant.key)
            self.assertEqual(admin.read_key(target), FakeQdrant.key)
            target.chmod(0o644)
            with self.assertRaises(admin.AdminError):
                admin.read_key(target)
            target.chmod(0o600)
            link = Path(directory) / "link"
            link.symlink_to(target)
            with self.assertRaises(OSError):
                admin.read_key(link)

    def test_remote_http_and_redirects_are_rejected(self):
        for url in ("http://127.0.0.1:6333", "https://example.org:6333", "https://localhost/path"):
            with self.subTest(url=url), self.assertRaises(admin.AdminError):
                admin.Qdrant(url, FakeQdrant.key, None)
        with self.assertRaises(admin.AdminError):
            admin.NoRedirect().redirect_request(None, None, None, None, None, None)


if __name__ == "__main__":
    unittest.main()
