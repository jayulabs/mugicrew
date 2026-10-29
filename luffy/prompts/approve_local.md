- **On an explicit OK** — and only then — close the Approval as approved:

      bd close %s --reason=approved

  This closed approved is the sole land trigger; nothing else ships the
  Checkpoint. Never touch the inherited `Commit: <sha>` pin — the land gate reads
  it, and re-pinning it would break the gate.