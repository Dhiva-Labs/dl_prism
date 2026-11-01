#!/usr/bin/env bash
# Renders the two AUR PKGBUILDs for one release and proves they build.
#
# usage: GATEWAY_VERSION=x DESKTOP_VERSION=y aur.sh \
#          <prism linux-amd64> <prism linux-arm64> \
#          <desktop-engine linux-x64.tar.gz> <desktop-engine linux-arm64.tar.gz> <outdir>
#
# Fills dl-prism.PKGBUILD.in (the gateway) and dl-prism-desktop.PKGBUILD.in
# with the versions, the release download URL (release tags carry the
# gateway version) and the sha256 of the release files and of the
# repository files they fetch from the tag. Then, in an archlinux:latest
# container, generates each .SRCINFO, runs makepkg on the x86_64 sources (all
# placed next to the PKGBUILDs, so makepkg verifies the checksums without
# downloading), installs both results together and runs the gateway. Writes
# dl-prism-bin-<version>-aur.tar.gz and dl-prism-desktop-bin-<version>-aur.tar.gz,
# each holding PKGBUILD and .SRCINFO, ready to push to the AUR.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "$here/../.." && pwd)"
[ $# -eq 5 ] || { echo "usage: $0 <amd64> <arm64> <x64.tar.gz> <arm64.tar.gz> <outdir>" >&2; exit 2; }
: "${GATEWAY_VERSION:?set GATEWAY_VERSION}" "${DESKTOP_VERSION:?set DESKTOP_VERSION}"
gw_x64="$(readlink -f "$1")"; gw_arm64="$(readlink -f "$2")"
dt_x64="$(readlink -f "$3")"; dt_arm64="$(readlink -f "$4")"
mkdir -p "$5"; out="$(cd "$5" && pwd)"
sha() { sha256sum "$1" | cut -d' ' -f1; }
work="$(mktemp -d)"; trap 'rm -rf "$work"' EXIT

# render <component> <version> <x64 file> <arm64 file> <local name=repo path>...
render() {
  local component="$1" version="$2" f_x64="$3" f_arm64="$4"; shift 4
  local dir="$work/$component" sources="" sums="" deps="" pair
  mkdir -p "$dir"
  (
    COMPONENT="$component"
    # shellcheck source=/dev/null
    . "$here/../linux-distros/distro.conf"
    tag="$TAG_PREFIX$GATEWAY_VERSION"
    for pair in "$@"; do
      sources+="\"${pair%%=*}::\$_raw/${pair#*=}\" "
      sums+="'$(sha "$repo/${pair#*=}")' "
      cp "$repo/${pair#*=}" "$dir/${pair%%=*}"
    done
    for d in "${ARCH_DEPENDS[@]}"; do deps+="'$d' "; done
    sed -e "s|@VERSION@|$version|g" \
        -e "s|@SUMMARY@|${SUMMARY//\'/}|g" -e "s|@HOMEPAGE@|$HOMEPAGE|g" \
        -e "s|@LICENSE@|$LICENSE|g" -e "s|@ARCH_DEPENDS@|${deps% }|g" \
        -e "s|@GITHUB_REPO@|$GITHUB_REPO|g" -e "s|@TAG@|$tag|g" \
        -e "s|@RELEASE_URL@|https://github.com/$GITHUB_REPO/releases/download/$tag|g" \
        -e "s|@SOURCES@|${sources% }|g" -e "s|@SHA256SUMS@|${sums% }|g" \
        -e "s|@SHA_X64@|$(sha "$f_x64")|g" -e "s|@SHA_ARM64@|$(sha "$f_arm64")|g" \
        "$here/$NAME.PKGBUILD.in" > "$dir/PKGBUILD"
  )
  if grep -n '@[A-Z0-9_]*@' "$dir/PKGBUILD"; then echo "unfilled placeholder" >&2; exit 1; fi
}

render gateway "$GATEWAY_VERSION" "$gw_x64" "$gw_arm64" \
  README.md=README.md CHANGELOG.md=CHANGELOG.md prism.example.yaml=prism.example.yaml \
  LICENSE=LICENSE NOTICE=NOTICE
cp "$gw_x64" "$work/gateway/prism-$GATEWAY_VERSION-x86_64"
render desktop "$DESKTOP_VERSION" "$dt_x64" "$dt_arm64" \
  com.dhivalabs.prism.desktop.desktop=packaging/linux/com.dhivalabs.prism.desktop.desktop \
  com.dhivalabs.prism.desktop.png=packaging/linux/com.dhivalabs.prism.desktop.png \
  LICENSE=LICENSE NOTICE=NOTICE
cp "$dt_x64" "$work/desktop/dl-prism-desktop-$DESKTOP_VERSION-x86_64.tar.gz"

# The container hands /build to its builder user; the trap gives it back to
# the runner's user, or the tar below and the cleanup cannot read it.
docker run --rm -v "$work:/build" -e HOST_UID="$(id -u)" -e HOST_GID="$(id -g)" \
  archlinux:latest bash -euo pipefail -c '
  trap "chown -R $HOST_UID:$HOST_GID /build" EXIT
  pacman -Syu --noconfirm --needed -q base-devel namcap
  useradd -m builder
  chown -R builder /build
  for c in gateway desktop; do
    cd "/build/$c"
    su builder -c "makepkg --printsrcinfo > .SRCINFO"
    su builder -c "makepkg --noconfirm --nodeps -f"
    namcap PKGBUILD
    cat .SRCINFO
  done
  pacman -U --noconfirm /build/gateway/*-x86_64.pkg.tar.zst /build/desktop/*-x86_64.pkg.tar.zst
  namcap /build/gateway/*-x86_64.pkg.tar.zst /build/desktop/*-x86_64.pkg.tar.zst
  prism version
  ls -l /usr/bin/dl-prism-desktop /usr/lib/dl-prism-desktop
  if ldd /usr/lib/dl-prism-desktop/desktop | grep "not found"; then exit 1; fi
'
tar -C "$work/gateway" -czf "$out/dl-prism-bin-$GATEWAY_VERSION-aur.tar.gz" PKGBUILD .SRCINFO
tar -C "$work/desktop" -czf "$out/dl-prism-desktop-bin-$DESKTOP_VERSION-aur.tar.gz" PKGBUILD .SRCINFO
ls -l "$out"
