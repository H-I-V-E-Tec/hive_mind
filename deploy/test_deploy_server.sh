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
mkdir -p "$BUNDLE/deploy" "$BUNDLE/scripts" "$PRIVATE_ROOT" "$FAKE_BIN"
cp "$PROJECT_ROOT/deploy/qdrant-server.compose.yml" "$BUNDLE/deploy/"
cp "$PROJECT_ROOT/deploy/qdrant_admin.py" "$BUNDLE/deploy/"
cp "$PROJECT_ROOT/deploy/deploy_server.sh" "$BUNDLE/deploy/"
cp "$PROJECT_ROOT/deploy/hive-predeploy-backup.example" "$BUNDLE/deploy/"
cp "$PROJECT_ROOT/deploy/hive.service" "$BUNDLE/deploy/"
cp "$PROJECT_ROOT/deploy/hive.env.example" "$BUNDLE/deploy/"
cp "$PROJECT_ROOT/scripts/hive_backup.py" "$BUNDLE/scripts/"
printf '%040d\n' 1 > "$BUNDLE/REVISION"
# Fake hive binary for testing
printf '#!/bin/sh\necho ok\n' > "$BUNDLE/hive"
chmod 0755 "$BUNDLE/hive"
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
# systemctl records which release `current` points at when hive is restarted.
cat > "$FAKE_BIN/systemctl" <<'SH'
#!/usr/bin/env bash
if [ "$1" = restart ]; then
  printf 'systemctl restart %s current=%s\n' "$2" "$(readlink "$HIVE_SERVER_INSTALL_ROOT/current")" >> "$HIVE_TEST_LOG"
else
  printf 'systemctl %s\n' "$*" >> "$HIVE_TEST_LOG"
fi
SH
cat > "$FAKE_BIN/curl" <<'SH'
#!/usr/bin/env bash
printf 'curl %s\n' "$*" >> "$HIVE_TEST_LOG"
[ "${HIVE_TEST_MIND_HEALTH:-ok}" = ok ]
SH
cat > "$FAKE_BIN/journalctl" <<'SH'
#!/usr/bin/env bash
printf 'journalctl %s\n' "$*" >> "$HIVE_TEST_LOG"
SH
chmod 0755 "$FAKE_BIN/docker" "$FAKE_BIN/python3" "$TEST_ROOT/backup-hook" \
  "$FAKE_BIN/systemctl" "$FAKE_BIN/curl" "$FAKE_BIN/journalctl"
mkdir -p "$TEST_ROOT/systemd"

export PATH="$FAKE_BIN:$PATH"
export HIVE_TEST_LOG="$LOG"
export HIVE_SERVER_INSTALL_ROOT="$INSTALL_ROOT"
export HIVE_SERVER_PRIVATE_ROOT="$PRIVATE_ROOT"
export HIVE_DEPLOY_BACKUP_HOOK="$TEST_ROOT/backup-hook"
export HIVE_DEPLOY_HEALTH_ATTEMPTS=1
export HIVE_DEPLOY_HEALTH_DELAY=0
export HIVE_DEPLOY_HISTORY_DIR="$TEST_ROOT/history"
export HIVE_DEPLOY_TEST_MODE=1
export HIVE_SYSTEMD_UNIT_DIR="$TEST_ROOT/systemd"

# --- Test 1: Initial deploy with hive binary ---
bash "$BUNDLE/deploy/deploy_server.sh" --version v1.2.3 --initial
test "$(readlink "$INSTALL_ROOT/current")" = "$INSTALL_ROOT/releases/v1.2.3"
test ! -e "$INSTALL_ROOT/releases/v1.2.3/docs"
test -x "$INSTALL_ROOT/releases/v1.2.3/hive"
test -f "$INSTALL_ROOT/releases/v1.2.3/deploy/hive.service"

# --- Test 2: Upgrade runs backup ---
printf '%040d\n' 2 > "$BUNDLE/REVISION"
bash "$BUNDLE/deploy/deploy_server.sh" --version v1.2.4
test "$(readlink "$INSTALL_ROOT/current")" = "$INSTALL_ROOT/releases/v1.2.4"
grep -qx backup "$LOG"

# --- Test 3: Failed health check triggers rollback ---
printf '%040d\n' 3 > "$BUNDLE/REVISION"
if HIVE_TEST_HEALTH=fail bash "$BUNDLE/deploy/deploy_server.sh" --version v1.2.5; then
  printf 'deploy aceitou health check com falha\n' >&2
  exit 1
fi
test "$(readlink -f "$INSTALL_ROOT/current")" = "$(readlink -f "$INSTALL_ROOT/releases/v1.2.4")"
grep -Fq "$INSTALL_ROOT/releases/v1.2.4/deploy/qdrant-server.compose.yml up -d" "$LOG"

# --- Test 4: Deploy without hive binary (backward compat) ---
BUNDLE_NO_MIND="$TEST_ROOT/bundle-no-mind"
mkdir -p "$BUNDLE_NO_MIND/deploy" "$BUNDLE_NO_MIND/scripts"
cp "$PROJECT_ROOT/deploy/qdrant-server.compose.yml" "$BUNDLE_NO_MIND/deploy/"
cp "$PROJECT_ROOT/deploy/qdrant_admin.py" "$BUNDLE_NO_MIND/deploy/"
cp "$PROJECT_ROOT/deploy/deploy_server.sh" "$BUNDLE_NO_MIND/deploy/"
cp "$PROJECT_ROOT/deploy/hive-predeploy-backup.example" "$BUNDLE_NO_MIND/deploy/"
cp "$PROJECT_ROOT/scripts/hive_backup.py" "$BUNDLE_NO_MIND/scripts/"
printf '%040d\n' 4 > "$BUNDLE_NO_MIND/REVISION"
bash "$BUNDLE_NO_MIND/deploy/deploy_server.sh" --version v1.2.6
test "$(readlink "$INSTALL_ROOT/current")" = "$INSTALL_ROOT/releases/v1.2.6"
test ! -e "$INSTALL_ROOT/releases/v1.2.6/hive"

# --- Test 5: hive restarts on the NEW release and healthz uses loopback ---
printf 'HIVE_HTTP_ADDR=":9443"\n' > "$PRIVATE_ROOT/hive.env"
printf '%040d\n' 5 > "$BUNDLE/REVISION"
: > "$LOG"
bash "$BUNDLE/deploy/deploy_server.sh" --version v1.2.7
test "$(readlink "$INSTALL_ROOT/current")" = "$INSTALL_ROOT/releases/v1.2.7"
grep -Fqx "systemctl restart hive current=$INSTALL_ROOT/releases/v1.2.7" "$LOG"
grep -Fq "http://127.0.0.1:9443/healthz" "$LOG"
test -f "$TEST_ROOT/systemd/hive.service"

# --- Test 6: TLS settings switch the probe to https ---
printf 'HIVE_HTTP_ADDR=0.0.0.0:9443\nHIVE_HTTP_TLS_CERT=/x.crt\nHIVE_HTTP_TLS_KEY=/x.key\n' > "$PRIVATE_ROOT/hive.env"
printf '%040d\n' 6 > "$BUNDLE/REVISION"
: > "$LOG"
bash "$BUNDLE/deploy/deploy_server.sh" --version v1.2.8
grep -Fq "https://127.0.0.1:9443/healthz" "$LOG"

# --- Test 7: hive healthz failure rolls back link and service ---
printf '%040d\n' 7 > "$BUNDLE/REVISION"
: > "$LOG"
if HIVE_TEST_MIND_HEALTH=fail bash "$BUNDLE/deploy/deploy_server.sh" --version v1.2.9; then
  printf 'deploy aceitou /healthz com falha\n' >&2
  exit 1
fi
test "$(readlink -f "$INSTALL_ROOT/current")" = "$(readlink -f "$INSTALL_ROOT/releases/v1.2.8")"
grep -Fqx "systemctl restart hive current=$INSTALL_ROOT/releases/v1.2.9" "$LOG"
grep -q '^systemctl restart hive current=.*/releases/v1.2.8$' "$LOG"
grep -q '^journalctl -u hive' "$LOG"

# --- Test 8: failed initial deploy leaves no current link ---
FRESH_ROOT="$TEST_ROOT/fresh-install"
printf '%040d\n' 8 > "$BUNDLE/REVISION"
if HIVE_SERVER_INSTALL_ROOT="$FRESH_ROOT" HIVE_TEST_MIND_HEALTH=fail \
    bash "$BUNDLE/deploy/deploy_server.sh" --version v1.3.0 --initial; then
  printf 'deploy inicial aceitou /healthz com falha\n' >&2
  exit 1
fi
test ! -e "$FRESH_ROOT/current" && test ! -L "$FRESH_ROOT/current"

printf 'deploy tests: ok\n'
