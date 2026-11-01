#!/usr/bin/env bash
# Installs the DL Prism Desktop Flatpak bundle the way a user would and runs
# it.
#
# usage: test-bundle.sh <bundle.flatpak>
#
# Runs on a plain Ubuntu runner with flatpak, Xvfb, xdotool and ImageMagick
# installed. The runtime comes from Flathub through the bundle's runtime
# repository, exactly as for `flatpak install --user ./<bundle>`. Then:
# every ELF file in the app finds its libraries inside the sandbox; the
# bundled gateway serves (and is reachable from the host, through the
# network permission); and the companion passes the repository's Xvfb smoke
# test (window up, libapp.so mapped, a rendered screenshot).
set -euo pipefail
[ $# -eq 1 ] || { echo "usage: $0 <bundle.flatpak>" >&2; exit 2; }
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
bundle="$(readlink -f "$1")"
app=com.dhivalabs.prism.desktop

flatpak remote-add --user --if-not-exists flathub https://dl.flathub.org/repo/flathub.flatpakrepo
flatpak install --user -y --noninteractive "$bundle"
flatpak info --user "$app"

echo "::group::ldd of every ELF file in /app, in the sandbox"
flatpak run --command=sh "$app" -c '
  set -eu
  n=0; missing=0
  # Flutter plugin libraries keep the RUNPATH of the build machine; at run
  # time the $ORIGIN/lib of the executable finds libflutter_linux_gtk.so.
  libs=/app/lib/dl-prism-desktop/lib
  for f in /app/bin/prism $(find /app/lib/dl-prism-desktop -type f); do
    [ "$(head -c 4 "$f" | od -An -tx1 | tr -d " \n")" = 7f454c46 ] || continue
    n=$((n + 1))
    if LD_LIBRARY_PATH="$libs" ldd "$f" 2>&1 | grep "not found" > /dev/null; then
      echo "missing libraries for $f:"; LD_LIBRARY_PATH="$libs" ldd "$f" | grep "not found"; missing=1
    fi
  done
  echo "$n ELF files checked with ldd"
  [ "$n" -ge 3 ] && [ "$missing" = 0 ]
'
echo "::endgroup::"

echo "::group::the bundled gateway"
flatpak run --command=prism "$app" version
log="$RUNNER_TEMP/prism-serve.log"
flatpak run --command=prism "$app" serve -c /app/share/doc/dl-prism/prism.example.yaml \
  --listen 127.0.0.1:18131 --offline > "$log" 2>&1 &
pid=$!
for _ in $(seq 1 30); do
  if curl -fsS http://127.0.0.1:18131/healthz > /dev/null 2>&1; then break; fi
  sleep 1
done
curl -fsS http://127.0.0.1:18131/healthz
curl -fsS http://127.0.0.1:18131/ | grep -qi '<div id="root"'
kill "$pid"
cat "$log"
echo "::endgroup::"

echo "::group::the companion under Xvfb"
SMOKE_SHOT="$RUNNER_TEMP/flatpak-desktop.png" \
  bash "$here/../linux-distros/smoke-desktop.sh" flatpak run "$app"
echo "::endgroup::"
echo "flatpak: OK"
