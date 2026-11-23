#!/usr/bin/env bash
# Build gitdeck.
#
#   scripts/build.sh                 this machine -> build/bin/gitdeck
#   scripts/build.sh --all [VERSION] release matrix -> build/dist/* + SHA256SUMS
#
# VERSION defaults to `git describe`. CI passes the tag being released, so
# local and CI builds go through exactly the same steps.
set -euo pipefail

cd "$(dirname "$0")/.."

readonly PLATFORMS=(
  darwin/amd64 darwin/arm64
  linux/amd64 linux/arm64
  windows/amd64 windows/arm64
)

version_or_describe() {
  if [ -n "${1:-}" ]; then
    echo "$1"
  else
    git describe --tags --always --dirty 2>/dev/null || echo dev
  fi
}

ldflags() { echo "-s -w -X main.version=$1"; }

build_host() {
  local version
  version="$(version_or_describe "${1:-}")"
  mkdir -p build/bin
  go build -trimpath -ldflags "$(ldflags "$version")" -o build/bin/gitdeck .
  echo "build/bin/gitdeck ($version)"
}

build_all() {
  local version
  version="$(version_or_describe "${1:-}")"
  rm -rf build/dist
  mkdir -p build/dist
  for platform in "${PLATFORMS[@]}"; do
    local goos="${platform%/*}" goarch="${platform#*/}"
    local out="build/dist/gitdeck-${version}-${goos}-${goarch}"
    [ "$goos" = windows ] && out+=".exe"
    # Pure Go, no cgo: every target is a plain GOOS/GOARCH cross-compile and
    # the Linux binaries come out static.
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
      go build -trimpath -ldflags "$(ldflags "$version")" -o "$out" .
    echo "$out"
  done
  (cd build/dist && sha256sum gitdeck-* > SHA256SUMS)
  echo "build/dist/SHA256SUMS"
}

case "${1:-}" in
--all) build_all "${2:-}" ;;
"") build_host ;;
*) echo "usage: scripts/build.sh [--all [VERSION]]" >&2; exit 2 ;;
esac
