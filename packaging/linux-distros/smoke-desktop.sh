#!/usr/bin/env bash
# Runs inside each distribution container (in-container.sh) against the
# installed desktop companion (or, in the AppImage check, the AppImage): the
# repository's own Xvfb smoke test (integrations/desktop/tool/smoke_xvfb.sh).
# The window title is set by the native GTK runner before any Dart code runs,
# so the title alone proves little. What proves the AOT snapshot executed:
# libapp.so is mapped into the app's process, the process stays alive, and
# the window shows a Flutter-rendered screen (many distinct colours).
#
# usage: smoke-desktop.sh <command> [args...]
set -euo pipefail

if [ -z "${SMOKE_INNER:-}" ]; then
  SMOKE_INNER=1 exec xvfb-run -a -s "-screen 0 1280x800x24" \
    dbus-run-session -- bash "$0" "$@"
fi

app=("$@")
title='^DL Prism Desktop$'
shot="${SMOKE_SHOT:-/tmp/dl-prism-desktop.png}"
log="$(mktemp)"

"${app[@]}" > "$log" 2>&1 &
pid=$!
for i in $(seq 1 90); do
  if xdotool search --name "$title" > /dev/null 2>&1; then
    echo "window '$title' is up after ${i}s"
    sleep 10
    kill -0 "$pid" || { echo "${app[*]} died after showing its window"; cat "$log"; exit 1; }
    import -window root "$shot"
    colours="$(identify -format %k "$shot")"
    echo "screenshot has $colours distinct colours"
    # The AppImage runtime and `flatpak run` start the app as a child
    # process, so look for libapp.so in every process rather than only $pid.
    mapped="$(grep -l 'libapp\.so' /proc/[0-9]*/maps 2> /dev/null || :)"
    [ -n "$mapped" ] || { echo "libapp.so is not mapped"; cat "$log"; exit 1; }
    echo "libapp.so mapped in: $mapped"
    kill "$pid"
    echo "--- app output"
    cat "$log"
    [ "$colours" -ge 20 ] || { echo "window looks blank"; exit 1; }
    exit 0
  fi
  kill -0 "$pid" 2> /dev/null || { echo "${app[*]} exited early"; cat "$log"; exit 1; }
  sleep 1
done
echo "no window '$title' after 90s"
cat "$log"
exit 1
