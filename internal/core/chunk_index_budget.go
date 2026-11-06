package core

import (
	"fmt"
	"io"

	"github.com/scigolib/hdf5/internal/utils"
)

// blockBudget caps the bytes one walk over a linked file structure reads:
// a chunk index (fixed and extensible arrays, version 1 and 2 B-trees) or a
// fractal heap. Every block the walk reads is charged before it is read, and
// the total may not pass the readable end of the file.
//
// The rule follows from the format, not from a tuning value: the blocks of
// one structure are separate allocations in the file (H5MF_alloc gives each
// block its own address range), so together they cannot be larger than the
// file. The C library needs no such cap because its metadata cache keys each
// entry by address and reads it once, and it checks a block only against the
// end of allocation (H5C__verify_len_eoa, H5Centry.c:949-988). Blocks that
// overlap are not detected there either: the cache has no overlap check.
//
// A visited set alone does not bound the work. Blocks at distinct but
// overlapping addresses each pass it, and a block of up to maxIndexBlock
// bytes repeated at a small stride turns a file of a few megabytes into
// gigabytes of reads and checksums (review of edb53b1, F1). Refusing overlap
// outright would need an interval set over every block. The budget gives the
// bound that matters, total work linear in the file's size, at the cost of
// one addition per block.
type blockBudget struct {
	spent uint64
	limit uint64 // Readable end of the file; 0 when unknown, then nothing is refused.
}

// newBlockBudget returns the budget of one walk over r, limited to the
// readable end of the file as dataLimit resolves it (ADR-003).
func newBlockBudget(r io.ReaderAt, sb *Superblock) *blockBudget {
	return &blockBudget{limit: dataLimit(r, sb)}
}

// WalkBudget is the blockBudget of one walk over a linked structure read
// outside this package: the group B-tree and its symbol table nodes
// (internal/structures). The same rule applies there: version 1 B-tree nodes
// carry no checksum, so nodes at distinct but overlapping addresses pass a
// visited set and only the budget bounds the walk (deep review of v0.14.3).
type WalkBudget struct {
	b *blockBudget
}

// NewWalkBudget returns the budget of one walk over r, limited to the readable
// end of the file.
func NewWalkBudget(r io.ReaderAt, sb *Superblock) *WalkBudget {
	return &WalkBudget{b: newBlockBudget(r, sb)}
}

// Charge counts n bytes of the block at addr, about to be read; see
// blockBudget.charge. A nil WalkBudget charges nothing.
func (w *WalkBudget) Charge(n, addr uint64, what string) error {
	if w == nil {
		return nil
	}
	return w.b.charge(n, addr, what)
}

// charge counts n bytes of the block at addr, about to be read, against the
// budget. what names the structure in the error. A nil budget charges
// nothing. Running over wraps utils.ErrCorrupt: a valid file cannot.
func (b *blockBudget) charge(n, addr uint64, what string) error {
	if b == nil {
		return nil
	}

	total, err := utils.SafeAdd(b.spent, n)
	if err != nil || (b.limit > 0 && total > b.limit) {
		return fmt.Errorf("%w: %s blocks claim more than the file's %d bytes (block of %d bytes at 0x%X)",
			utils.ErrCorrupt, what, b.limit, n, addr)
	}
	b.spent = total

	return nil
}
