#!/usr/bin/env bash
# Empacotamento compartilhado pela matriz de release.
set -Eeuo pipefail

: "${GOOS:?GOOS obrigatório}"
: "${GOARCH:?GOARCH obrigatório}"
: "${RELEASE_VERSION:?RELEASE_VERSION obrigatório}"
[[ "$RELEASE_VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] || {
  printf 'versão inválida\n' >&2
  exit 1
}

if [ "$GOOS" = linux ] && [ "$GOARCH" = arm64 ]; then
  export CC=aarch64-linux-gnu-gcc
fi

rm -rf dist/package
mkdir -p dist/package
extension=''
[ "$GOOS" != windows ] || extension='.exe'
binary="hive-mind${extension}"
asset="hive-mind-${RELEASE_VERSION}-${GOOS}-${GOARCH}"

CGO_ENABLED=1 go build -trimpath -buildvcs=true \
  -ldflags="-X main.Version=${RELEASE_VERSION} -X main.SourceRevision=${GITHUB_SHA} -s -w" \
  -o "dist/package/$binary" .

if [ "$GOOS" = windows ]; then
  python3 - "dist/package/$binary" "dist/${asset}.zip" <<'PY'
import os, sys, zipfile
with zipfile.ZipFile(sys.argv[2], "w", compression=zipfile.ZIP_DEFLATED) as archive:
    archive.write(sys.argv[1], os.path.basename(sys.argv[1]))
PY
else
  tar -C dist/package -czf "dist/${asset}.tar.gz" "$binary"
fi
rm -rf dist/package
