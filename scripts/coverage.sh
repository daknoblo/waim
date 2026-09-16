#!/usr/bin/env bash
set -euo pipefail

directory="${1:-coverage}"
mkdir -p "$directory"

go test -coverpkg=./... -coverprofile="$directory/all.out" ./...
awk 'NR == 1 || $1 !~ /_templ\.go:/' "$directory/all.out" > "$directory/handwritten.out"

go tool cover -func="$directory/all.out" > "$directory/all-functions.txt"
go tool cover -func="$directory/handwritten.out" > "$directory/handwritten-functions.txt"

printf '\nAll Go statements (including generated templates):\n'
awk '/^total:/ { print $3 }' "$directory/all-functions.txt"
printf 'Handwritten Go statements (excluding *_templ.go):\n'
awk '/^total:/ { print $3 }' "$directory/handwritten-functions.txt"
printf '\nProfiles and per-function reports: %s\n' "$directory"
printf 'HTML view: go tool cover -html="%s/handwritten.out"\n' "$directory"
