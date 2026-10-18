# Plan usage history is usage.read events written by the TUI poller

Amends ADR 0231 (mugicrew-tui-logs-holds-the-newest-1000-events…) §3.

The Stats tab's burn chart (ADR 0259, mugicrew-the-stats-tab-burn-chart-is-a-block-area-chart…) needs a history of Plan usage reads. Decision:

- **Store:** one `usage.read` event per good read in the event log, under proc `tui`, with flat attrs `five_hour_pct`, `five_hour_resets_at`, `weekly_pct`, `weekly_resets_at`. An absent window or zero reset omits its attrs; there is no top-level `kind`, so the f picker never lists it.
- **Digest:** `usage.read` joins `inDigest` and `digestNeedles`, amending ADR 0231 §3's event set, so full scan, tail extend and rotation rebuild carry it with no second scan. stats.Sum, landEvents and the finished-flash filter by Msg and are unaffected.
- **Retention:** rotation-bounded per ADR 0028 (mugicrew-event-log-rotation): about 17 days per 10 MiB file today. A week is not guaranteed, because the cap is in bytes.
- **Writer:** the TUI poller only (background poll, Stats entry, f5), where it folds a good read into its last-good cache. No TUI running means a gap in the record. Near-duplicates from two TUIs are dropped at read, not write.

Rejected: a loop-side poller (new behaviour on the account's API); a separate store file; a separate `usage.read` scan beside the digest.
