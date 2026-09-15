#!/usr/bin/env bash
set -euo pipefail

case "${GITHUB_REF_TYPE:-}:${GITHUB_REF_NAME:-}" in
  branch:main)
    channel=stable
    version="stable-$(date -u +%Y%m%d-%H%M)"
    ;;
  branch:develop)
    channel=dev
    version="dev-$(date -u +%Y%m%d-%H%M)"
    ;;
  tag:*)
    version="${GITHUB_REF_NAME#v}"
    if ! [[ "$version" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
      echo "::error::Only stable X.Y.Z version tags are publishable." >&2
      exit 1
    fi
    if ! git merge-base --is-ancestor HEAD refs/remotes/origin/main; then
      echo "::error::Version tags must point to a commit approved on main." >&2
      exit 1
    fi
    channel=stable
    ;;
  *)
    echo "::error::Only main, develop and approved version tags may publish." >&2
    exit 1
    ;;
esac

printf 'channel=%s\nversion=%s\n' "$channel" "$version"
