package bd

import (
	"log/slog"

	"jayulabs.org/mugicrew/contract"
)

// stripTitle is the one place package bd strips a kind prefix from a title it
// writes (ADR 0097). Only when the title changed does it emit title.stripped on
// the default logger (the event log, never stderr), with attrs naming the bead
// id or the parent/creator.
func stripTitle(title string, attrs ...any) string {
	s := contract.StripKindPrefix(title)
	if s != title {
		slog.Warn("title.stripped", append([]any{"from", title, "to", s}, attrs...)...)
	}
	return s
}
