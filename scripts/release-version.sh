#!/usr/bin/env bash
# Every version string in the repository, derived from one tag. Bash rather
# than zsh because this runs on Linux and macOS release runners alike.

set -euo pipefail

usage() {
  echo 'usage: scripts/release-version.sh v1.2.3[-rc.N]' >&2
  exit 2
}

[ $# -eq 1 ] || usage

tag="${1#v}"
core="${tag%%-*}"

[[ "$core" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || {
  echo "not a semantic version: $1" >&2
  exit 2
}

major="${core%%.*}"
rest="${core#*.}"
minor="${rest%%.*}"
patch="${rest##*.}"

# 99 keeps a release above every candidate for the same version, so installing
# a release over rc.2 is still an update to iOS rather than a no-op.
if [ "$tag" = "$core" ]; then
  suffix=99
else
  suffix="${tag##*.}"
  [[ "$suffix" =~ ^[0-9]+$ ]] || {
    echo "candidate has no number: $1" >&2
    exit 2
  }
  [ "$suffix" -lt 99 ] || {
    echo "candidate number must be below 99: $1" >&2
    exit 2
  }
fi

echo "MARKETING_VERSION=$core"
echo "CURRENT_PROJECT_VERSION=$(( (major * 10000 + minor * 100 + patch) * 100 + suffix ))"
