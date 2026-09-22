#!/usr/bin/env bash
# Instala e promove um bundle do servidor Hive já verificado pelo pull_server_release.sh.
set -Eeuo pipefail
umask 077

INSTALL_ROOT="${HIVE_SERVER_INSTALL_ROOT:-/opt/hive-test}"
PRIVATE_ROOT="${HIVE_SERVER_PRIVATE_ROOT:-/srv/hive-private}"
BACKUP_HOOK="${HIVE_DEPLOY_BACKUP_HOOK:-/usr/local/sbin/hive-predeploy-backup}"
HEALTH_ATTEMPTS="${HIVE_DEPLOY_HEALTH_ATTEMPTS:-12}"
HEALTH_DELAY="${HIVE_DEPLOY_HEALTH_DELAY:-5}"
HISTORY_DIR="${HIVE_DEPLOY_HISTORY_DIR:-/var/lib/hive-deploy}"
PROJECT="hive-server"
VERSION=""
INITIAL=false

die() { printf 'ERRO: %s\n' "$*" >&2; exit 1; }

while [ "$#" -gt 0 ]; do
  case "$1" in
    --version) [ "$#" -ge 2 ] || die "--version exige valor"; VERSION="$2"; shift 2 ;;
    --initial) INITIAL=true; shift ;;
    *) die "argumento desconhecido: $1" ;;
  esac
done

[[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] || die "versão inválida"
[[ "$HEALTH_ATTEMPTS" =~ ^[1-9][0-9]*$ ]] || die "tentativas de health check inválidas"
[[ "$HEALTH_DELAY" =~ ^[0-9]+$ ]] || die "intervalo de health check inválido"
if [ "$(id -u)" -ne 0 ]; then
  if [ "${HIVE_DEPLOY_TEST_MODE:-}" != 1 ] || [[ "$INSTALL_ROOT" != /tmp/* ]] || [[ "$PRIVATE_ROOT" != /tmp/* ]]; then
    die "execute como root"
  fi
fi
command -v docker >/dev/null || die "docker não encontrado"
command -v python3 >/dev/null || die "python3 não encontrado"

SOURCE_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
[ -f "$SOURCE_ROOT/REVISION" ] || die "bundle sem REVISION"
REVISION="$(tr -d '\r\n' < "$SOURCE_ROOT/REVISION")"
[[ "$REVISION" =~ ^[0-9a-f]{40}$ ]] || die "revisão inválida no bundle"

TARGET="$INSTALL_ROOT/releases/$VERSION"
CURRENT_LINK="$INSTALL_ROOT/current"
OLD_RELEASE=""
if [ -L "$CURRENT_LINK" ]; then
  OLD_RELEASE="$(readlink -f "$CURRENT_LINK")"
elif [ -e "$CURRENT_LINK" ]; then
  die "$CURRENT_LINK deve ser um link simbólico"
fi

install -d -m 0755 "$INSTALL_ROOT/releases"
if [ -e "$TARGET" ]; then
  [ -f "$TARGET/REVISION" ] || die "release existente incompleta: $TARGET"
  [ "$(tr -d '\r\n' < "$TARGET/REVISION")" = "$REVISION" ] || die "versão já existe com outra revisão"
else
  install -d -m 0755 "$TARGET/deploy" "$TARGET/scripts" "$TARGET/docs/operations"
  install -m 0644 "$SOURCE_ROOT/REVISION" "$TARGET/REVISION"
  install -m 0644 "$SOURCE_ROOT/deploy/qdrant-server.compose.yml" "$TARGET/deploy/qdrant-server.compose.yml"
  install -m 0755 "$SOURCE_ROOT/deploy/qdrant_admin.py" "$TARGET/deploy/qdrant_admin.py"
  install -m 0755 "$SOURCE_ROOT/deploy/deploy_server.sh" "$TARGET/deploy/deploy_server.sh"
  install -m 0755 "$SOURCE_ROOT/deploy/hive-predeploy-backup.example" "$TARGET/deploy/hive-predeploy-backup.example"
  install -m 0755 "$SOURCE_ROOT/scripts/hive_backup.py" "$TARGET/scripts/hive_backup.py"
  install -m 0644 "$SOURCE_ROOT/docs/operations/delivery-and-deployment.md" \
    "$TARGET/docs/operations/delivery-and-deployment.md"
fi

COMPOSE_FILE="$TARGET/deploy/qdrant-server.compose.yml"
docker compose -p "$PROJECT" -f "$COMPOSE_FILE" config --quiet
[ -f "$PRIVATE_ROOT/qdrant.env" ] || die "falta $PRIVATE_ROOT/qdrant.env"
[ -f "$PRIVATE_ROOT/admin.key" ] || die "falta $PRIVATE_ROOT/admin.key"
[ -f "$PRIVATE_ROOT/ca.crt" ] || die "falta $PRIVATE_ROOT/ca.crt"

if [ -n "$OLD_RELEASE" ] && [ "$OLD_RELEASE" != "$TARGET" ]; then
  [ -x "$BACKUP_HOOK" ] || die "configure o hook de backup executável: $BACKUP_HOOK"
  "$BACKUP_HOOK"
elif [ -z "$OLD_RELEASE" ] && [ "$INITIAL" != true ]; then
  die "primeiro deploy exige --initial; migrações existentes exigem cadastrar a release atual antes"
fi

ROLLBACK_NEEDED=false
rollback() {
  status=$?
  trap - ERR
  if [ "$ROLLBACK_NEEDED" = true ] && [ -n "$OLD_RELEASE" ] && [ -f "$OLD_RELEASE/deploy/qdrant-server.compose.yml" ]; then
    printf 'Deploy falhou; restaurando release anterior %s\n' "$OLD_RELEASE" >&2
    docker compose -p "$PROJECT" -f "$OLD_RELEASE/deploy/qdrant-server.compose.yml" up -d --remove-orphans || true
  fi
  exit "$status"
}
trap rollback ERR

docker compose -p "$PROJECT" -f "$COMPOSE_FILE" pull qdrant
ROLLBACK_NEEDED=true
docker compose -p "$PROJECT" -f "$COMPOSE_FILE" up -d --remove-orphans

healthy=false
for _ in $(seq 1 "$HEALTH_ATTEMPTS"); do
  if python3 "$TARGET/deploy/qdrant_admin.py" \
      --key-file "$PRIVATE_ROOT/admin.key" --ca-file "$PRIVATE_ROOT/ca.crt" health >/dev/null 2>&1; then
    healthy=true
    break
  fi
  sleep "$HEALTH_DELAY"
done
if [ "$healthy" != true ]; then
  printf 'ERRO: Qdrant não passou no health check autenticado\n' >&2
  false
fi

NEW_LINK="$INSTALL_ROOT/.current.$$"
ln -s "$TARGET" "$NEW_LINK"
mv -Tf "$NEW_LINK" "$CURRENT_LINK"
ROLLBACK_NEEDED=false

install -d -m 0700 "$HISTORY_DIR"
printf '{"version":"%s","revision":"%s","result":"healthy"}\n' "$VERSION" "$REVISION" \
  >> "$HISTORY_DIR/history.jsonl"
printf 'Deploy concluído: %s (%s)\n' "$VERSION" "$REVISION"
