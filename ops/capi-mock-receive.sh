#!/usr/bin/env bash
set -Eeuo pipefail

original_command="${SSH_ORIGINAL_COMMAND:-}"
sha="${original_command#upload }"
if [[ "$original_command" != "upload $sha" || ! "$sha" =~ ^[0-9a-f]{40}$ ]]; then
  echo "expected: upload <full lowercase commit SHA>" >&2
  exit 2
fi

base=/data/capi-mock/incoming/$sha
install -d -o capi-mock-upload -g capi-mock-upload -m 0750 "$base"
tmp="$base/capi-mock.tmp"
cat > "$tmp"
size=$(stat -c '%s' "$tmp")
if (( size < 1024 || size > 52428800 )); then
  echo "uploaded binary size is outside the allowed range" >&2
  exit 1
fi
chmod 0500 "$tmp"
mv -f "$tmp" "$base/capi-mock"
echo "uploaded release for $sha"
