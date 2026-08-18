#!/bin/zsh
# Every version string in the repository, derived from one tag.

set -euo pipefail

[[ $# -eq 1 ]] || {
  print -u2 'usage: scripts/release-version.sh v1.2.3[-rc.N]'
  exit 2
}

tag="${1#v}"
core="${tag%%-*}"
candidate="${tag#*-}"

[[ "$core" =~ '^[0-9]+\.[0-9]+\.[0-9]+$' ]] || {
  print -u2 "not a semantic version: $1"
  exit 2
}

major="${core%%.*}"
patch="${core##*.}"
minor="${${core#*.}%.*}"

# 99 keeps a release above every candidate for the same version.
if [[ "$candidate" == "$tag" ]]; then
  suffix=99
else
  suffix="${candidate##*.}"
  [[ "$suffix" =~ '^[0-9]+$' ]] || { print -u2 "candidate has no number: $1"; exit 2 }
  (( suffix < 99 )) || { print -u2 "candidate number must be below 99: $1"; exit 2 }
fi

print "MARKETING_VERSION=$core"
print "CURRENT_PROJECT_VERSION=$(( (major * 10000 + minor * 100 + patch) * 100 + suffix ))"
