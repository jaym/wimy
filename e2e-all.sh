#!/usr/bin/env bash
# Runs every e2e suite against headless river. Linux only.
set -u
cd "$(dirname "$0")"

mkdir -p bin
for c in wimy wimyctl keyinject ptrinject; do
  go build -o "bin/$c" "./cmd/$c" || exit 1
done

failed=()
for s in e2e.sh e2e-multi.sh e2e-keys.sh e2e-layer.sh e2e-deco.sh e2e-mouse.sh e2e-reload.sh; do
  echo "=== $s"
  if ! "./$s"; then
    failed+=("$s")
  fi
done

echo
if [ ${#failed[@]} -ne 0 ]; then
  echo "FAILED: ${failed[*]}"
  exit 1
fi
echo "all e2e suites passed"
