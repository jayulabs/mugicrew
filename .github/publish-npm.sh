#!/usr/bin/env bash
# usage: publish-npm.sh <tarball-dir> <version> <event-name>
# Publishes the mugicrew npm packages at <version> that are not yet on npm, platforms
# first. When every package is already published nothing is published: a push run fails
# (a forgotten version bump), a workflow_dispatch run only warns.
set -euo pipefail
dist=$1 version=$2 event=$3

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
