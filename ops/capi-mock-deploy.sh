#!/usr/bin/env bash
set -Eeuo pipefail

base=/data/capi-mock
releases="$base/releases"
incoming="$base/incoming"
sha="${1:-}"

if [[ ! "$sha" =~ ^[0-9a-f]{40}$ ]]; then
  echo "expected a full lowercase commit SHA" >&2
  exit 2
fi
install -d -o root -g root -m 0755 "$base" "$releases"
install -d -o capi-mock-upload -g capi-mock-upload -m 0750 "$incoming"
exec 9>"$base/deploy.lock"
flock 9

release="$releases/$sha"
if [[ ! -x "$release/capi-mock" ]]; then
  artifact="$incoming/$sha/capi-mock"
  [[ -s "$artifact" ]] || { echo "uploaded release not found for $sha" >&2; exit 1; }
  mkdir -p "$release"
  install -o root -g root -m 0755 "$artifact" "$release/capi-mock"
fi

previous=""
if [[ -L "$base/current" ]]; then
  previous=$(readlink -f "$base/current")
fi
ln -s "$release" "$base/current.next"
mv -Tf "$base/current.next" "$base/current"
if ! systemctl restart capi-mock.service || ! systemctl is-active --quiet capi-mock.service; then
  if [[ -n "$previous" ]]; then
    ln -s "$previous" "$base/current.rollback"
    mv -Tf "$base/current.rollback" "$base/current"
    systemctl restart capi-mock.service || true
  else
    rm -f "$base/current"
  fi
  echo "service failed after release activation; rollback attempted" >&2
  exit 1
fi

for attempt in {1..15}; do
  if curl --noproxy '*' --fail --silent http://127.0.0.1:3221/healthz | python3 -c 'import json,sys; sys.exit(0 if json.load(sys.stdin).get("version") == sys.argv[1] else 1)' "$sha"; then
    printf '%s\n' "$sha" > "$base/DEPLOYED_COMMIT.next"
    mv -f "$base/DEPLOYED_COMMIT.next" "$base/DEPLOYED_COMMIT"
    python3 -c 'from pathlib import Path; import shutil,sys; shutil.rmtree(Path(sys.argv[1]), ignore_errors=True)' "$incoming/$sha"
    echo "deployed $sha"
    exit 0
  fi
  sleep 2
done

if [[ -n "$previous" ]]; then
  ln -s "$previous" "$base/current.rollback"
  mv -Tf "$base/current.rollback" "$base/current"
  systemctl restart capi-mock.service || true
else
  rm -f "$base/current"
fi
echo "health/version check failed; rollback attempted" >&2
exit 1
