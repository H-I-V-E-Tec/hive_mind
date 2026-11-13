import hashlib
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


INSTALLER = Path(__file__).resolve().parents[1] / "install.sh"


@unittest.skipUnless(os.name == "posix", "POSIX installer")
class InstallSignatureTests(unittest.TestCase):
    def run_installer(self, signature):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            commands = root / "commands"
            commands.mkdir()
            for name in ("tr", "mktemp", "rm", "awk", "sha256sum"):
                executable = shutil.which(name)
                if not executable:
                    self.skipTest(f"requires {name}")
                (commands / name).symlink_to(executable)

            def script(name, body):
                path = commands / name
                path.write_text("#!/bin/sh\n" + body)
                path.chmod(0o700)

            script("uname", 'case "$1" in -s) echo Linux;; -m) echo x86_64;; esac\n')
            script("tar", 'echo extraction-attempted; exit 99\n')
            script("curl", '''while [ "$#" -gt 0 ]; do
    if [ "$1" = "-o" ]; then shift; output="$1"; fi
    shift
done
case "$output" in
    */SHA256SUMS) printf '%s  hive-v9.9.9-linux-amd64.tar.gz\\n' "$TEST_HASH" > "$output";;
    *.sigstore.json) printf '{}' > "$output";;
    *) printf 'synthetic-archive' > "$output";;
esac
''')
            if signature is not None:
                script("cosign", f"exit {signature}\n")
            env = {
                "PATH": str(commands), "HOME": str(root), "TMPDIR": str(root),
                "VERSION": "v9.9.9",
                "TEST_HASH": hashlib.sha256(b"synthetic-archive").hexdigest(),
            }
            return subprocess.run(
                ["/bin/sh", str(INSTALLER)], env=env, text=True,
                stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=10,
            )

    def test_missing_cosign_stops_before_download(self):
        result = self.run_installer(None)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("cosign is required", result.stdout)
        self.assertNotIn("Downloading", result.stdout)

    def test_invalid_signature_stops_before_extraction(self):
        result = self.run_installer(1)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Checksum verified", result.stdout)
        self.assertIn("Sigstore signature verification failed", result.stdout)
        self.assertNotIn("extraction-attempted", result.stdout)


if __name__ == "__main__":
    unittest.main()
