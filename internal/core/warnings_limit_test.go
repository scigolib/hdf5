package core

import "testing"

// fillWarnings records maxWarnings findings without a checksum span, so every
// later finding is suppressed.
func fillWarnings(sb *Superblock) {
	for i := 0; i < maxWarnings; i++ {
		sb.Warnf(WarnLegacyDenseLayout, uint64(0x1000+i), "dense layout %d", i)
	}
}

// A finding suppressed past the limit must still be visible by code: the
// write path refuses files by code (review of 0238904, R3).
func TestWarnings_HasWarningSeesSuppressedCodes(t *testing.T) {
	sb := &Superblock{}
	fillWarnings(sb)
	sb.Warnf(WarnHeaderChecksum, 0x9000, "header without checksum")

	for _, w := range sb.Warnings() {
		if w.Code == WarnHeaderChecksum {
			t.Fatalf("the finding should have been suppressed from the list, got %v", w)
		}
	}
	if !sb.HasWarning(WarnHeaderChecksum) {
		t.Fatal("HasWarning(WarnHeaderChecksum) = false for a suppressed finding")
	}
	if sb.HasWarning(WarnLegacyDatatype) {
		t.Fatal("HasWarning reports a code that was never recorded")
	}
}

// Suppressed findings without a span leave the checksum list complete; a
// suppressed legacy checksum makes it incomplete even when no span was kept.
func TestWarnings_LegacyChecksumsCompleteness(t *testing.T) {
	sb := &Superblock{}
	fillWarnings(sb)
	sb.Warnf(WarnHeaderChecksum, 0x9000, "header without checksum")
	if spans, complete := sb.LegacyChecksums(); len(spans) != 0 || !complete {
		t.Fatalf("after suppressed non-checksum findings: spans=%d complete=%v, want 0 true", len(spans), complete)
	}

	sb.warnLegacyChecksumf(WarnLegacyMetadataChecksum, 0xA000, 64, "CRC32 block")
	if spans, complete := sb.LegacyChecksums(); len(spans) != 0 || complete {
		t.Fatalf("after a suppressed legacy checksum: spans=%d complete=%v, want 0 false", len(spans), complete)
	}
}

func TestWarnings_NilSuperblock(t *testing.T) {
	var sb *Superblock
	if sb.HasWarning(WarnHeaderChecksum) {
		t.Fatal("nil superblock reports a warning")
	}
}
