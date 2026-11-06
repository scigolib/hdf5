package hdf5

import (
	"errors"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
)

// fullWarningList returns a superblock whose warning list is already full of
// findings that refuse nothing, so the next findings are suppressed.
func fullWarningList() *core.Superblock {
	sb := &core.Superblock{}
	for i := 0; i < 1000; i++ {
		sb.Warnf(core.WarnLegacyDenseLayout, uint64(0x1000+i), "dense layout %d", i)
	}
	return sb
}

// Review of 0238904, R3: a header without a checksum found after the warning
// list is full must still refuse OpenForWrite.
func TestRefuseUnrepairableHeaders_SuppressedFinding(t *testing.T) {
	sb := fullWarningList()
	sb.Warnf(core.WarnHeaderChecksum, 0x9000, "header without checksum")
	f := &File{sb: sb}

	err := refuseUnrepairableHeaders(f)
	if !errors.Is(err, ErrNeedsRepack) {
		t.Fatalf("refuseUnrepairableHeaders = %v, want ErrNeedsRepack", err)
	}
}
