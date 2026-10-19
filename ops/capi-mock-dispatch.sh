#!/usr/bin/env bash
set -Eeuo pipefail

base=/data/capi-mock
sha="${1:-}"

if [[ ! "$sha" =~ ^[0-9a-f]{40}$ ]]; then
  echo "expected a full lowercase commit SHA" >&2
  exit 2
fi

exec 9>"$base/dispatch.lock"
flock -n 9 || { echo "deployment dispatch is already running"; exit 0; }
unit="capi-mock-deploy-$sha.service"
if systemctl is-active --quiet "$unit"; then
  echo "deployment for $sha is already running"
  exit 0
fi

systemd-run --quiet --collect --no-block \
  --unit="capi-mock-deploy-$sha" \
  --property=TimeoutStartSec=infinity \
  "$base/deploy.sh" "$sha"
echo "queued deployment for $sha"
