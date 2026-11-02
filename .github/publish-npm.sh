#!/usr/bin/env bash
# usage: publish-npm.sh <tarball-dir> <version> <event-name> [<before> <sha>]
# Publishes the mugicrew npm packages at <version> that are not yet on npm, platforms
# first. When every package is already published nothing is published: a push run fails
# (a forgotten version bump), a workflow_dispatch run only warns, and a push whose
# <before>..<sha> diff is non-empty and entirely under site/ warns (nothing to release).
# Any doubt (no or all-zero <before>, a failed or empty diff) counts as not site-only.
set -euo pipefail
dist=$1 version=$2 event=$3 before=${4:-} sha=${5:-}

# site_only succeeds only when git diff --name-only <before> <sha> is non-empty and every
# path is under site/.
site_only() {
  [ "$event" = push ] && [ -n "$sha" ] || return 1
  case "$before" in *[!0]*) ;; *) return 1 ;; esac # empty or all zeros
  local files
  files=$(git diff --name-only "$before" "$sha" 2>/dev/null) || return 1
  [ -n "$files" ] || return 1
  ! printf '%s\n' "$files" | grep -qv '^site/'
}

pkgs=(mugicrew-linux-x64 mugicrew-linux-arm64 mugicrew-darwin-arm64 mugicrew-darwin-x64 mugicrew)

published=()
unpublished=0
for p in "${pkgs[@]}"; do
  # a view miss (non-zero exit, empty or other output) counts as not published
  if [ "$(npm view "@jayulabs/$p@$version" version 2>/dev/null || true)" = "$version" ]; then
    published+=(1)
  else
    published+=(0)
    unpublished=$((unpublished + 1))
  fi
done

if [ "$unpublished" -eq 0 ]; then
  if [ "$event" = workflow_dispatch ]; then
    echo "::warning::Nothing was published: every @jayulabs/mugicrew* package at $version is already published."
    exit 0
  fi
  if site_only; then
    echo "::warning::Nothing was published: this push changed only site/**, so there is nothing to release."
    exit 0
  fi
  echo "::error::Every @jayulabs/mugicrew* package at $version is already published, so nothing was published. Bump the version in all five npm package manifests and re-publish; see docs/release.md."
  exit 1
fi

for i in "${!pkgs[@]}"; do
  p=${pkgs[$i]}
  if [ "${published[$i]}" = 1 ]; then
    echo "@jayulabs/$p@$version already published, skipping"
    continue
  fi
  npm publish "$dist/jayulabs-$p-$version.tgz" --access public
done
