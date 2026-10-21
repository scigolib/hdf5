package core

import (
	"fmt"
	"io"
	"math"

	"github.com/scigolib/hdf5/internal/utils"
)

// undefinedAddress is HDF5's "undefined address" sentinel: the all-ones value
// of the file's offset field (H5_addr_defined/HADDR_UNDEF in the C library).
// It is not a location, and arithmetic on it wraps, so a block read must reject
// it before doing anything else.
const undefinedAddress uint64 = 0xFFFFFFFFFFFFFFFF

// maxDatasetSize is the hard cap on a dataset's in-memory representation:
// 1 TB, exactly the limit the chunked path has applied since CVE-2025-7067
// (utils.MaxChunkSize*1024). ADR-003 keeps one policy for datasets instead of
// a second, looser one for the contiguous path.
const maxDatasetSize uint64 = utils.MaxChunkSize * 1024

// dataLimit resolves the end of the readable address space for r, in the order
// ADR-003 prescribes: the reader's own size when known (utils.ReaderSize: a
// logical EndOfFile first, then Stat/Size), otherwise the superblock EOF
// address, otherwise 0 (unknown, so callers skip the region check and keep only
// the hard cap).
//
// The reader's size wins over EOFAddress because the superblock's EOA is only
// refreshed when the file is written and closed: during a read-modify-write
// session it is stale the moment the allocator grows the file. A writer reports
// its live end through EndOfFile, which is exactly the H5FD_get_eoa value the
// C library checks against; a plain *os.File reports what is on disk. This is
// the Go counterpart of H5F_get_eoa / H5FD_get_eof in the C library.
func dataLimit(r io.ReaderAt, sb *Superblock) uint64 {
	if size, ok := utils.ReaderSize(r); ok {
		return size
	}

	if sb != nil {
		return sb.EOFAddress
	}

	return 0
}

// ReadBlock is the single funnel for block reads — the Go counterpart of
// H5F_block_read (H5Fio.c:105-142). It validates [addr, addr+size) against the
// file limit and maxSize, then allocates and reads.
//
// A zero size reads nothing and returns an empty slice. Any other size is
// rejected when the address is undefined, when addr+size overflows or reaches
// past the limit, when size exceeds maxSize, or when the address or size cannot
// be represented on this platform. The last check exists because
// make([]byte, n) throws when n exceeds int, and a Go allocation failure kills
// the process instead of returning an error, unlike the malloc-returns-NULL
// path every C decoder relies on (ADR-003).
//
// A short read is an error. This deviates deliberately from the C VFD, which
// zero-fills past EOF (H5FD__sec2_read, memset at H5FDsec2.c:669): there the
// buffer is pre-sized for a file the caller already knows, while here the size
// comes from file metadata, so a short read is evidence of corruption and
// hiding it behind zeros would let a caller decode fabricated data.
func ReadBlock(r io.ReaderAt, sb *Superblock, addr, size, maxSize uint64, what string) ([]byte, error) {
	if size == 0 {
		return []byte{}, nil
	}

	if addr == undefinedAddress {
		return nil, fmt.Errorf("%s: undefined address", what)
	}

	if err := utils.ValidateBufferSize(size, maxSize, what); err != nil {
		return nil, err
	}

	if err := utils.ValidateRegion(addr, size, dataLimit(r, sb), what); err != nil {
		return nil, err
	}

	// The cast to int64 below turns an address above MaxInt64 negative, which
	// would read the wrong region; reject it before the conversion.
	if addr > math.MaxInt64 {
		return nil, fmt.Errorf("%s: address %d exceeds the platform limit", what, addr)
	}

	if size > uint64(math.MaxInt) {
		return nil, fmt.Errorf("%s: size %d exceeds the platform limit", what, size)
	}

	buf := make([]byte, size)
	n, err := r.ReadAt(buf, int64(addr))
	if err != nil {
		return nil, fmt.Errorf("%s: read at offset %d size %d: %w", what, addr, size, err)
	}

	if n < len(buf) {
		return nil, fmt.Errorf("%s: short read at offset %d: got %d of %d bytes", what, addr, n, size)
	}

	return buf, nil
}

// contigDataSize computes and validates the byte size of a contiguous dataset,
// mirroring H5D__contig_check (H5Dcontig.c:360-416) in three steps: the
// overflow-checked element-count multiplication, the dataset hard cap, and the
// file-region check.
//
// The C library detects the multiplication overflow by division
// (nelmts != data_size / dt_size, H5Dcontig.c:395) and then rejects
// addr+data_size past rel_eoa (H5Dcontig.c:408-411); utils.SafeMultiply and
// utils.ValidateRegion make the same checks explicit. An empty dataset has
// nothing to read and no region to validate, so it returns a zero size.
func contigDataSize(r io.ReaderAt, sb *Superblock, addr, nelmts, elemSize uint64, what string) (uint64, error) {
	total, err := utils.SafeMultiply(nelmts, elemSize)
	if err != nil {
		return 0, fmt.Errorf("%s: dataset size overflow: %w", what, err)
	}

	if total == 0 {
		return 0, nil
	}

	if err := utils.ValidateBufferSize(total, maxDatasetSize, what); err != nil {
		return 0, err
	}

	if err := utils.ValidateRegion(addr, total, dataLimit(r, sb), what); err != nil {
		return 0, err
	}

	return total, nil
}

// readContiguousDatasetData is the one path every dataset reader takes for a
// contiguous layout: contigDataSize validates the metadata-derived size and
// region, then ReadBlock performs the bounded read. Keeping both steps here is
// what stops the next reader from copying the unchecked make+ReadAt pattern
// that ADR-003 had to remove from four places.
func readContiguousDatasetData(r io.ReaderAt, sb *Superblock, layout *DataLayoutMessage, nelmts, elemSize uint64) ([]byte, error) {
	dataSize, err := contigDataSize(r, sb, layout.DataAddress, nelmts, elemSize, "dataset data")
	if err != nil {
		return nil, err
	}

	return ReadBlock(r, sb, layout.DataAddress, dataSize, maxDatasetSize, "dataset data")
}
