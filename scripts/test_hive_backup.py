import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import hive_backup as backup


class BackupTests(unittest.TestCase):
    def test_rejects_remote_plaintext_credentials_and_redirects(self):
        for value in ("http://10.0.0.1:6333", "https://user:secret@host", "https://host/?api-key=secret"):
            with self.assertRaises(backup.BackupError):
                backup.service_url(value)
        self.assertEqual(backup.service_url("https://qdrant.internal:6333"), "https://qdrant.internal:6333")
        with self.assertRaises(backup.BackupError):
            backup.NoRedirect().redirect_request(None)

    def test_pair_integrity_and_identity(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            for name in ("data.snapshot", "control.snapshot"):
                (root / name).write_bytes(b"synthetic snapshot")
            manifest = {"schema_version": 1, "hive_id": "hive", "collection": "data", "files": {name: backup.digest(root / name) for name in ("data.snapshot", "control.snapshot")}}
            (root / "manifest.json").write_text(json.dumps(manifest))
            backup.verify_pair(root, "hive", "data")
            with self.assertRaises(backup.BackupError):
                backup.verify_pair(root, "other", "data")
            (root / "control.snapshot").write_bytes(b"tampered")
            with self.assertRaises(backup.BackupError):
                backup.verify_pair(root, "hive", "data")

    def test_backup_requires_quiesced_writer_before_network(self):
        with patch.object(backup, "Qdrant") as qdrant:
            with self.assertRaises(backup.BackupError):
                backup.backup({}, Path("."), "hive", "data")
            qdrant.assert_not_called()

    def test_failed_second_snapshot_never_commits_partial_pair(self):
        with tempfile.TemporaryDirectory() as temporary, patch.object(backup, "Qdrant") as qdrant, patch.object(backup, "restic") as restic:
            qdrant.return_value.snapshot.side_effect = [None, backup.BackupError("unavailable")]
            with self.assertRaises(backup.BackupError):
                backup.backup({"HIVE_BACKUP_WRITER_STOPPED": "yes"}, Path(temporary), "hive", "data")
            restic.assert_not_called()


if __name__ == "__main__":
    unittest.main()
