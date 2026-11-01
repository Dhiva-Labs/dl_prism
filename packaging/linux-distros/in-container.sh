#!/usr/bin/env bash
# Runs inside a distribution container started by verify.sh.
# MODE: rpm (Fedora) | rpm-suse (openSUSE) | pacman (Arch) | appimage (Fedora)
# FILE: the package to install, under /pkgs.
set -euo pipefail
# shellcheck source=/dev/null
. /linux-distros/distro.conf

# Display and GL stack for the GUI smoke test, per distribution.
case "$MODE" in
  rpm|appimage)
    dnf -y -q install file findutils which procps-ng \
      xorg-x11-server-Xvfb xorg-x11-xauth mesa-dri-drivers dbus-daemon \
      "${SMOKE_DEPS_DNF[@]}"
    ;;
  rpm-suse)
    zypper -n -q --gpg-auto-import-keys refresh
    # xvfb-run needs awk, and dbus-run-session is in dbus-1-daemon there.
    zypper -n -q install file findutils which procps gawk \
      xvfb-run Mesa-dri dbus-1 dbus-1-daemon \
      "${SMOKE_DEPS_ZYPPER[@]}"
    ;;
  pacman)
    # The archlinux image skips /usr/share/doc (and man, locale) on install
    # to stay small; a real Arch system does not, and the gateway smoke reads
    # the example config the package installs there.
    sed -i '/^NoExtract/d' /etc/pacman.conf
    pacman -Syu --noconfirm --needed -q file findutils which procps-ng \
      xorg-server-xvfb xorg-xauth mesa dbus "${SMOKE_DEPS_PACMAN[@]}"
    ;;
esac

# /with (optional) holds packages of this repository that the package
# depends on (the desktop companion needs the gateway); they are installed
# in the same transaction, as a user would.
shopt -s nullglob
with_rpm=(/with/*."$(uname -m)".rpm)
with_pkg=(/with/*-"$(uname -m)".pkg.tar.zst)
shopt -u nullglob
case "$MODE" in
  rpm)      dnf -y install "$FILE" "${with_rpm[@]}"; files() { rpm -ql "$NAME"; } ;;
  rpm-suse) zypper -n install --allow-unsigned-rpm "$FILE" "${with_rpm[@]}"; files() { rpm -ql "$NAME"; } ;;
  pacman)   pacman -U --noconfirm "$FILE" "${with_pkg[@]}"; files() { pacman -Qlq "$NAME"; } ;;
  appimage)
    # The libraries an AppImage expects from the host: a desktop's GTK 3 and
    # X11/Wayland stack, plus APPIMAGE_HOST_PKGS from distro.conf.
    dnf -y -q install gtk3 "${APPIMAGE_HOST_PKGS[@]}"
    cp "$FILE" /tmp/app.AppImage
    chmod +x /tmp/app.AppImage
    (cd /tmp && /tmp/app.AppImage --appimage-extract > /dev/null)
    files() { find /tmp/squashfs-root -type f; }
    # The smoke commands call the program by its installed name; here that
    # name runs the AppImage, with the arguments the command gives it.
    read -r -a words <<< "${GUI_CMD:-$SMOKE_CMD}"
    printf '#!/bin/sh\nexec /tmp/app.AppImage "$@"\n' > "/usr/local/bin/${words[0]}"
    chmod 0755 "/usr/local/bin/${words[0]}"
    export APPIMAGE_EXTRACT_AND_RUN=1
    ;;
  *) echo "unknown MODE $MODE" >&2; exit 2 ;;
esac

# Every ELF file the package installed must find all of its libraries. The
# package's own library directories go on LD_LIBRARY_PATH, as the app's
# RPATH/$ORIGIN would supply them at run time, so only libraries missing
# from the distribution are reported.
mapfile -t elfs < <(files | while IFS= read -r f; do
  [ -f "$f" ] && [ ! -L "$f" ] || continue
  [ "$(head -c 4 "$f" | od -An -tx1 | tr -d ' \n')" = 7f454c46 ] && echo "$f"
done)
libdirs="$(for f in "${elfs[@]}"; do case "$f" in *.so|*.so.*) dirname "$f" ;; esac; done | sort -u | paste -sd:)"
missing=0
for f in "${elfs[@]}"; do
  if LD_LIBRARY_PATH="$libdirs" ldd "$f" 2>&1 | grep 'not found' > /dev/null; then
    echo "missing libraries for $f:"
    LD_LIBRARY_PATH="$libdirs" ldd "$f" | grep 'not found'
    missing=1
  fi
done
echo "${#elfs[@]} ELF files checked with ldd"
[ "$missing" = 0 ] || exit 1

# Smoke test, as shell commands from distro.conf. CLI tools: SMOKE_CMD must
# exit 0. GUI apps: GUI_CMD must still be running after SMOKE_SECONDS under
# Xvfb, with no loader errors.
export HOME=/tmp/smoke-home
mkdir -p "$HOME"
if [ -n "${SMOKE_CMD:-}" ]; then
  bash -euo pipefail -c "$SMOKE_CMD"
fi
if [ -n "${GUI_CMD:-}" ]; then
  cat > /tmp/smoke.sh <<'EOS'
log=/tmp/smoke.log
"$@" > "$log" 2>&1 &
pid=$!
secs="${SMOKE_SECONDS:-20}"
for _ in $(seq 1 "$secs"); do
  kill -0 "$pid" 2> /dev/null || break
  sleep 1
done
if kill -0 "$pid" 2> /dev/null; then
  echo "still running after ${secs}s"
  kill "$pid"
  ok=1
else
  wait "$pid" && rc=0 || rc=$?
  echo "exited early with status $rc"
  ok=0
fi
cat "$log"
if grep -E 'error while loading shared libraries|cannot open shared object|undefined symbol' "$log"; then ok=0; fi
[ "$ok" = 1 ]
EOS
  export SMOKE_SECONDS
  xvfb-run -a -s '-screen 0 1280x800x24' dbus-run-session -- bash /tmp/smoke.sh bash -c "exec $GUI_CMD"
fi
echo "$MODE: OK"
