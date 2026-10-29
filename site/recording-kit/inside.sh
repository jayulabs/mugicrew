#!/usr/bin/env bash
# Runs inside the recording image: seeds the fixture, records every tape into
# /out, and cuts a poster frame from each MP4. Each tape records against a
# fresh fixture, since a scene may change it.
set -euo pipefail
DEMO=/tmp/demo
for tape in /kit/tapes/*.tape; do
  name=$(basename "$tape" .tape)
  rm -rf "$DEMO"
  # A tape marked "# fixture: none" runs in the container's own state.
  grep -q '^# fixture: none' "$tape" || bash /kit/seed.sh "$DEMO"
  echo "recording $name"
  vhs "$tape"
  # A frame from the first third of the clip: past the launch, before any exit.
  dur=$(ffprobe -v error -show_entries format=duration -of csv=p=0 "/out/$name.mp4")
  ffmpeg -y -v error -ss "$(awk -v d="$dur" 'BEGIN{printf "%.2f", d/3}')" -i "/out/$name.mp4" -frames:v 1 -q:v 3 "/out/$name.jpg"
done
