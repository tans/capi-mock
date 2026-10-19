#!/usr/bin/env bash
set -Eeuo pipefail

base=/data/capi-mock
repository="$base/repository.git"
releases="$base/releases"
sha="${1:-}"
go_bin=/opt/go1.26.1/bin/go

if [[ ! "$sha" =~ ^[0-9a-f]{40}$ ]]; then
  echo "expected a full lowercase commit SHA" >&2
  exit 2
fi
[[ -x "$go_bin" ]] || { echo "Go compiler not found: $go_bin" >&2; exit 1; }
install -d -o root -g root -m 0755 "$base" "$releases" "$base/tmp"
exec 9>"$base/deploy.lock"
flock 9

if [[ ! -d "$repository" ]]; then
  git clone --bare https://github.com/tans/capi-mock.git "$repository"
fi
git --git-dir="$repository" fetch --prune origin +refs/heads/main:refs/remotes/origin/main
git --git-dir="$repository" cat-file -e "$sha^{commit}"
git --git-dir="$repository" merge-base --is-ancestor "$sha" refs/remotes/origin/main || {
  echo "commit is not reachable from origin/main" >&2
  exit 1
}

release="$releases/$sha"
if [[ ! -x "$release/capi-mock" ]]; then
  tmp=$(mktemp -d "$base/tmp/$sha.XXXXXX")
  trap 'rm -rf "$tmp"' EXIT
  git --git-dir="$repository" archive "$sha" | tar -x -C "$tmp"
  mkdir -p "$release"
  if ! (cd "$tmp" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 "$go_bin" build -trimpath -ldflags="-s -w -X main.version=$sha" -o "$release/capi-mock" ./cmd/capi-mock); then
    rm -rf "$release"
    exit 1
  fi
  chmod 0755 "$release/capi-mock"
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
  if curl --noproxy '*' --fail --silent http://127.0.0.1:3221/healthz >/dev/null; then
    printf '%s\n' "$sha" > "$base/DEPLOYED_COMMIT.next"
    mv -f "$base/DEPLOYED_COMMIT.next" "$base/DEPLOYED_COMMIT"
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
echo "health check failed; rollback attempted" >&2
exit 1
