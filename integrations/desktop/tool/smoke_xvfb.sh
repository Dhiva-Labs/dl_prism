#!/usr/bin/env bash
# Start a Flutter Linux app on a virtual display and check it really runs.
#
#   tool/smoke_xvfb.sh <app-binary> <window-title-regex> <screenshot.png>
#
# Needs xvfb, xauth, xdotool, dbus-x11 and imagemagick. The window title is set
# by the native GTK runner before any Dart code runs, so the title alone proves
# little. What proves the AOT snapshot executed: libapp.so is mapped into the
# process, the process stays alive, and the window shows a Flutter-rendered
# screen (the screenshot has many distinct colours).
set -euo pipefail

if [ -z "${SMOKE_INNER:-}" ]; then
  SMOKE_INNER=1 exec xvfb-run -a -s "-screen 0 1280x800x24" \
    dbus-run-session -- bash "$0" "$@"
fi

app="$1"
title="$2"
shot="$3"
log="$(mktemp)"
mkdir -p "$(dirname "$shot")"

"$app" > "$log" 2>&1 &
pid=$!
for i in $(seq 1 90); do
  if xdotool search --name "$title" > /dev/null 2>&1; then
    echo "window '$title' is up after ${i}s"
    sleep 10
    kill -0 "$pid" || { echo "$app died after showing its window"; cat "$log"; exit 1; }
    import -window root "$shot"
    colours="$(identify -format %k "$shot")"
    echo "screenshot has $colours distinct colours"
    echo "--- mapped libraries"
    awk '{print $6}' "/proc/$pid/maps" | grep -E 'libapp\.so|libflutter_linux_gtk|libwebkit2gtk' | sort -u || true
    grep -q 'libapp\.so' "/proc/$pid/maps" || { echo "libapp.so is not mapped"; cat "$log"; exit 1; }
    kill "$pid"
    echo "--- app output"
    cat "$log"
    [ "$colours" -ge 20 ] || { echo "window looks blank"; exit 1; }
    exit 0
  fi
  kill -0 "$pid" 2> /dev/null || { echo "$app exited early"; cat "$log"; exit 1; }
  sleep 1
done
echo "no window '$title' after 90s"
cat "$log"
exit 1
