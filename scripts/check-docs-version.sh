#!/usr/bin/env bash
set -euo pipefail

if grep -rnE 'ghcr\.io/daknoblo/waim:v[0-9]' README.md docs/; then
  echo "::error::Image versions must use plain semver, without a v prefix." >&2
  exit 1
fi

documented=$(grep -rhoE 'ghcr\.io/daknoblo/waim:[0-9]+\.[0-9]+\.[0-9]+' README.md docs/ \
  | sed 's/.*://' | sort -u || true)

while IFS= read -r version; do
  [ -n "$version" ] || continue
  ref="refs/tags/$version"
  if ! git rev-parse --verify "$ref" >/dev/null 2>&1; then
    ref="refs/tags/v$version"
  fi
  if ! git rev-parse --verify "$ref" >/dev/null 2>&1 ||
    ! git merge-base --is-ancestor "$ref" refs/remotes/origin/main; then
    echo "::error::Docs pin $version, which is not a version tag approved on main." >&2
    exit 1
  fi
  echo "Docs pin approved version $version."
done <<< "$documented"
