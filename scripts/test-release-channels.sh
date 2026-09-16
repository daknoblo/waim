#!/usr/bin/env bash
set -euo pipefail

scripts=$(cd "$(dirname "$0")" && pwd)
fixture=$(mktemp -d)
trap 'rm -r -- "$fixture"' EXIT

git init -q -b main "$fixture/work"
cd "$fixture/work"
git config user.name "Release test"
git config user.email "release-test@example.invalid"
git -c commit.gpgsign=false commit -q --allow-empty -m initial
git update-ref refs/remotes/origin/main HEAD
git tag 1.0.0
git tag v1.0.1

expect_channel() {
  local actual
  actual=$(GITHUB_REF_TYPE="$1" GITHUB_REF_NAME="$2" bash "$scripts/release-channel.sh")
  if ! grep -qx "channel=$3" <<< "$actual" || ! grep -Eq "^version=$4$" <<< "$actual"; then
    echo "unexpected channel output: $actual" >&2
    exit 1
  fi
}

reject_channel() {
  if GITHUB_REF_TYPE="$1" GITHUB_REF_NAME="$2" bash "$scripts/release-channel.sh" >/dev/null 2>&1; then
    echo "incorrectly accepted $1:$2" >&2
    exit 1
  fi
}

expect_channel branch main stable 'stable-[0-9]{8}-[0-9]{4}'
expect_channel branch develop dev 'dev-[0-9]{8}-[0-9]{4}'
expect_channel tag 1.0.0 stable '1\.0\.0'
expect_channel tag v1.0.1 stable '1\.0\.1'
reject_channel branch feature/test
reject_channel tag 1.0.0-rc.1
reject_channel tag 01.0.0

mkdir docs
printf 'image: ghcr.io/daknoblo/waim:1.0.0\n' > README.md
bash "$scripts/check-docs-version.sh"
printf 'image: ghcr.io/daknoblo/waim:1.0.1\n' > README.md
bash "$scripts/check-docs-version.sh"
printf 'image: ghcr.io/daknoblo/waim:9.0.0\n' > README.md
if bash "$scripts/check-docs-version.sh" >/dev/null 2>&1; then
  echo "incorrectly accepted unpublished documentation version" >&2
  exit 1
fi

git switch -q -c develop
git -c commit.gpgsign=false commit -q --allow-empty -m development
git tag 2.0.0
reject_channel tag 2.0.0
expect_channel branch develop dev 'dev-[0-9]{8}-[0-9]{4}'
printf 'image: ghcr.io/daknoblo/waim:2.0.0\n' > README.md
if bash "$scripts/check-docs-version.sh" >/dev/null 2>&1; then
  echo "incorrectly accepted a development tag in stable documentation" >&2
  exit 1
fi
if bash "$scripts/check-release-ready.sh" >/dev/null 2>&1; then
  echo "incorrectly allowed make release on develop" >&2
  exit 1
fi

git switch -q main
git add README.md
git -c commit.gpgsign=false commit -q -m documentation
git init -q --bare "$fixture/origin.git"
git remote add origin "$fixture/origin.git"
git push -q origin main
bash "$scripts/check-release-ready.sh"
printf '\ndirty\n' >> README.md
if bash "$scripts/check-release-ready.sh" >/dev/null 2>&1; then
  echo "incorrectly allowed a release from a dirty main" >&2
  exit 1
fi
git add README.md
git -c commit.gpgsign=false commit -q -m unapproved
if bash "$scripts/check-release-ready.sh" >/dev/null 2>&1; then
  echo "incorrectly allowed a release from unpushed main" >&2
  exit 1
fi

echo "Release channel tests passed."
