- **On an explicit OK** — and only then — make sure the Decision Packet is written to the
  Approval (the body-file step above), then stop. Do not close the Approval: when this
  session exits, the Checkpoint's Merge Request opens as a GitLab Draft carrying the
  Packet, and the human's un-draft of it on GitLab is the approval. Never touch the
  inherited `Commit: <sha>` pin — the land gate reads it, and re-pinning it would break the gate.