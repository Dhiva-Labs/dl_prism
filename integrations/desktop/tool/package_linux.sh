#!/usr/bin/env bash
# Pack the bundles that tool/build_variants.sh leaves in dist/ into tarballs:
#
#   tool/package_linux.sh [OUT_DIR]
#
#   dl-prism-desktop-full-<version>-linux-<arch>.tar.gz     embedded dashboard window
#   dl-prism-desktop-engine-<version>-linux-<arch>.tar.gz   browser-only dashboard
#
# <version> is the pubspec version; <arch> is x64 or arm64, taken from the
# machine, so the same script serves both runners.
set -euo pipefail

cd "$(dirname "$0")/.."

OUT_DIR="${1:-dist/tarballs}"

case "$(uname -m)" in
  x86_64) ARCH=x64 ;;
  aarch64 | arm64) ARCH=arm64 ;;
  *)
    echo "error: unsupported machine '$(uname -m)' (x86_64 or aarch64)" >&2
    exit 1
    ;;
esac

VERSION="$(grep '^version:' pubspec.yaml | awk '{print $2}' | cut -d+ -f1)"

mkdir -p "$OUT_DIR"
for variant in full engine; do
  dir="dl-prism-desktop-$variant"
  [ -x "dist/$dir/desktop" ] || {
    echo "error: dist/$dir/desktop not found; run tool/build_variants.sh first" >&2
    exit 1
  }
  name="$dir-$VERSION-linux-$ARCH"
  tar -czf "$OUT_DIR/$name.tar.gz" --owner=0 --group=0 --sort=name \
    --transform "s,^$dir,$name," -C dist "$dir"
  echo "$OUT_DIR/$name.tar.gz"
done
