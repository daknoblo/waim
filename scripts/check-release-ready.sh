#!/usr/bin/env bash
set -euo pipefail

if [ "$(git branch --show-current)" != main ]; then
  echo "error: releases must be tagged on main after PR approval, never on develop" >&2
  exit 1
fi
if [ -n "$(git status --porcelain)" ]; then
  echo "error: commit changes through a PR before tagging a release" >&2
  exit 1
fi

git fetch origin main --tags
if [ "$(git rev-parse HEAD)" != "$(git rev-parse refs/remotes/origin/main)" ]; then
  echo "error: local main must match origin/main; pull --ff-only before tagging" >&2
  exit 1
fi
