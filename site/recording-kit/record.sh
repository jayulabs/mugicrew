#!/usr/bin/env bash
# The one command: builds the recording image on the fresh-install container,
# seeds the fixture and runs every tape inside it, writing MP4s and poster
# frames into the site's static assets. Run by hand from anywhere, in the
# devShell (podman). Never run in CI (ADR 0365).
set -euo pipefail
KIT=$(cd "$(dirname "$0")" && pwd)
OUT=$KIT/../static/recordings
ROOT=$(cd "$KIT/../../../.." && pwd)
mkdir -p "$OUT"

podman build -t localhost/mugicrew-fresh-install -f "$ROOT/containers/fresh-install/fedora.Containerfile" "$ROOT/containers/fresh-install"
podman build -t localhost/mugicrew-recorder --build-arg BASE=localhost/mugicrew-fresh-install -f "$KIT/Containerfile" "$KIT"
podman run --rm --userns=keep-id:uid=1000,gid=1000 -v "$OUT:/out:Z" localhost/mugicrew-recorder
