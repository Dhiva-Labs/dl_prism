#!/usr/bin/env bash
# Runs inside each distribution container (in-container.sh) against the
# installed dl-prism package, as the Builds workflow runs the binary it
# built: version, a routing dry run from the example config, then serve it
# and read /healthz, /admin/status and the embedded dashboard.
set -euo pipefail
cfg=/usr/share/doc/dl-prism/prism.example.yaml
prism version
prism explain -c "$cfg" "Write a Python dedupe function"
log="$(mktemp)"
prism serve -c "$cfg" --listen 127.0.0.1:18130 --offline > "$log" 2>&1 &
pid=$!
for _ in $(seq 1 30); do
  if curl -fsS http://127.0.0.1:18130/healthz > /dev/null 2>&1; then break; fi
  sleep 1
done
curl -fsS http://127.0.0.1:18130/healthz
curl -fsS http://127.0.0.1:18130/admin/status | tee /tmp/status.json
echo
grep -Eq '"version": *"' /tmp/status.json
curl -fsS http://127.0.0.1:18130/ | grep -qi '<div id="root"'
kill "$pid"
cat "$log"
echo "gateway: OK"
