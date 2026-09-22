#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/hive-deploy-test.XXXXXX")"
cleanup() { rm -rf -- "$TEST_ROOT"; }
trap cleanup EXIT

BUNDLE="$TEST_ROOT/bundle"
INSTALL_ROOT="$TEST_ROOT/install"
PRIVATE_ROOT="$TEST_ROOT/private"
FAKE_BIN="$TEST_ROOT/fake-bin"
LOG="$TEST_ROOT/commands.log"
mkdir -p "$BUNDLE/deploy" "$BUNDLE/scripts" "$BUNDLE/docs/operations" "$PRIVATE_ROOT" "$FAKE_BIN"
cp "$PROJECT_ROOT/deploy/qdrant-server.compose.yml" "$BUNDLE/deploy/"
cp "$PROJECT_ROOT/deploy/qdrant_admin.py" "$BUNDLE/deploy/"
cp "$PROJECT_ROOT/deploy/deploy_server.sh" "$BUNDLE/deploy/"
cp "$PROJECT_ROOT/deploy/hive-predeploy-backup.example" "$BUNDLE/deploy/"
cp "$PROJECT_ROOT/scripts/hive_backup.py" "$BUNDLE/scripts/"
cp "$PROJECT_ROOT/docs/operations/delivery-and-deployment.md" "$BUNDLE/docs/operations/"
printf '%040d\n' 1 > "$BUNDLE/REVISION"
: > "$PRIVATE_ROOT/qdrant.env"
: > "$PRIVATE_ROOT/admin.key"
: > "$PRIVATE_ROOT/ca.crt"

cat > "$FAKE_BIN/docker" <<'SH'
#!/usr/bin/env bash
printf 'docker %s\n' "$*" >> "$HIVE_TEST_LOG"
exit 0
SH
cat > "$FAKE_BIN/python3" <<'SH'
#!/usr/bin/env bash
[ "${HIVE_TEST_HEALTH:-ok}" = ok ]
SH
cat > "$TEST_ROOT/backup-hook" <<'SH'
#!/usr/bin/env bash
printf 'backup\n' >> "$HIVE_TEST_LOG"
SH
chmod 0755 "$FAKE_BIN/docker" "$FAKE_BIN/python3" "$TEST_ROOT/backup-hook"

export PATH="$FAKE_BIN:$PATH"
export HIVE_TEST_LOG="$LOG"
export HIVE_SERVER_INSTALL_ROOT="$INSTALL_ROOT"
export HIVE_SERVER_PRIVATE_ROOT="$PRIVATE_ROOT"
export HIVE_DEPLOY_BACKUP_HOOK="$TEST_ROOT/backup-hook"
export HIVE_DEPLOY_HEALTH_ATTEMPTS=1
export HIVE_DEPLOY_HEALTH_DELAY=0
export HIVE_DEPLOY_HISTORY_DIR="$TEST_ROOT/history"
export HIVE_DEPLOY_TEST_MODE=1

bash "$BUNDLE/deploy/deploy_server.sh" --version v1.2.3 --initial
test "$(readlink "$INSTALL_ROOT/current")" = "$INSTALL_ROOT/releases/v1.2.3"

printf '%040d\n' 2 > "$BUNDLE/REVISION"
bash "$BUNDLE/deploy/deploy_server.sh" --version v1.2.4
test "$(readlink "$INSTALL_ROOT/current")" = "$INSTALL_ROOT/releases/v1.2.4"
grep -qx backup "$LOG"

printf '%040d\n' 3 > "$BUNDLE/REVISION"
if HIVE_TEST_HEALTH=fail bash "$BUNDLE/deploy/deploy_server.sh" --version v1.2.5; then
  printf 'deploy aceitou health check com falha\n' >&2
  exit 1
fi
test "$(readlink "$INSTALL_ROOT/current")" = "$INSTALL_ROOT/releases/v1.2.4"
grep -Fq "$INSTALL_ROOT/releases/v1.2.4/deploy/qdrant-server.compose.yml up -d" "$LOG"
printf 'deploy tests: ok\n'
