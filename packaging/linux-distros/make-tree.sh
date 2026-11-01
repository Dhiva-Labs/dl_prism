#!/usr/bin/env bash
# Lays out the installed file tree that pack.sh (and the Flatpak) package,
# from what the Builds workflow compiled and tested (there is no .deb):
#
#   COMPONENT=gateway make-tree.sh <prism binary> <root>
#   COMPONENT=desktop make-tree.sh <dl-prism-desktop-engine-*.tar.gz> <root>
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "$here/../.." && pwd)"
[ $# -eq 2 ] || { echo "usage: COMPONENT=<gateway|desktop> $0 <input> <root>" >&2; exit 2; }
# shellcheck source=/dev/null
. "$here/distro.conf"
in="$1"
root="$2"

case "$COMPONENT" in
  gateway)
    install -Dm755 "$in" "$root/usr/bin/prism"
    for f in README.md CHANGELOG.md prism.example.yaml; do
      install -Dm644 "$repo/$f" "$root/usr/share/doc/$NAME/$f"
    done
    install -Dm644 "$repo/NOTICE" "$root/usr/share/licenses/$NAME/NOTICE"
    ;;
  desktop)
    # The tarball holds one directory: the Flutter bundle (desktop, lib/,
    # data/), which finds lib/ and data/ next to its executable.
    install -d "$root/usr/lib/$NAME"
    tar -xzf "$in" -C "$root/usr/lib/$NAME" --strip-components=1
    [ -x "$root/usr/lib/$NAME/desktop" ] || { echo "no desktop executable in $in" >&2; exit 1; }
    install -d "$root/usr/bin"
    ln -s "../lib/$NAME/desktop" "$root/usr/bin/$NAME"
    install -Dm644 "$repo/packaging/linux/com.dhivalabs.prism.desktop.desktop" \
      "$root/usr/share/applications/com.dhivalabs.prism.desktop.desktop"
    install -Dm644 "$repo/packaging/linux/com.dhivalabs.prism.desktop.png" \
      "$root/usr/share/icons/hicolor/192x192/apps/com.dhivalabs.prism.desktop.png"
    install -Dm644 "$repo/integrations/desktop/README.md" "$root/usr/share/doc/$NAME/README.md"
    install -Dm644 "$repo/NOTICE" "$root/usr/share/licenses/$NAME/NOTICE"
    ;;
esac
find "$root" -not -path "$root/usr/lib/$NAME/*" | sort
