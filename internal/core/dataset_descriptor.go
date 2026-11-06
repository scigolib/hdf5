package core

import (
	"fmt"
	"io"
	"math"

	"github.com/scigolib/hdf5/internal/utils"
)

// maxBTreeChunkBytes is the largest chunk a version 1 B-tree index can
// describe: its keys store the chunk size in 32 bits (H5Dchunk.c:1196-1199).
const maxBTreeChunkBytes uint64 = 0xFFFFFFFF

// DatasetDescriptor is the validated metadata of one dataset: its datatype,
// dataspace, storage layout and filter pipeline, parsed once from the object
// header and checked against each other and against the file before any read.
//
// It is the Go counterpart of the state the C library builds when a dataset is
// opened: H5D__open_oid (H5Dint.c:1766) reads the messages, and
// H5D__layout_oh_read (H5Dlayout.c:475) runs the layout's init callback, which
// validates the storage (H5D__contig_check, H5D__compact_init,
// H5D__chunk_init). Every later read works from that validated state; here
// every reader takes a descriptor instead of parsing messages itself, so no
// reader can reach storage bytes without the checks having run (ADR-004 §1).
//
// A descriptor is immutable after construction and safe for concurrent use.
// The accessors return the parsed messages; callers must not modify them.
type DatasetDescriptor struct {
	datatype       *DatatypeMessage
	dataspace      *DataspaceMessage
	layout         *DataLayoutMessage
	filterPipeline *FilterPipelineMessage

	nelmts   uint64 // Number of elements in the dataspace, overflow-checked.
	elemSize uint64 // Datatype size in bytes, never zero.

	// dataBytes is nelmts × elemSize, overflow-checked. For contiguous and
	// compact layouts it is also the size of storage that exists in the file.
	dataBytes uint64

	// chunkBytes is the byte size of one chunk (the product of all chunk
	// dimensions, the last of which is the element size); zero unless chunked.
	chunkBytes uint64
}

// NewDatasetDescriptor parses and validates the dataset messages of an object
// header. It is the only constructor: readers obtain storage bytes only through
// a descriptor it returned.
//
// The first message of each type is used, as H5O_msg_read does. Errors caused
// by metadata that contradicts the file or the format wrap utils.ErrCorrupt;
// a decoder error (for example an unsupported datatype) is passed through with
// context, not reclassified.
func NewDatasetDescriptor(r io.ReaderAt, header *ObjectHeader, sb *Superblock) (*DatasetDescriptor, error) {
	if header == nil || sb == nil {
		return nil, fmt.Errorf("dataset descriptor: object header and superblock are required")
	}

	msgs, err := findDatasetMessages(header)
	if err != nil {
		return nil, err
	}

	datatype, err := ParseDatatypeMessage(msgs.datatype.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse datatype: %w", err)
	}

	dataspace, err := ParseDataspaceMessage(msgs.dataspace.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse dataspace: %w", err)
	}

	layout, err := ParseDataLayoutMessage(msgs.layout.Data, sb)
	if err != nil {
		return nil, fmt.Errorf("failed to parse layout: %w", err)
	}

	var pipeline *FilterPipelineMessage
	if msgs.pipeline != nil {
		pipeline, err = ParseFilterPipelineMessage(msgs.pipeline.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse filter pipeline: %w", err)
		}
	}

	return newDatasetDescriptor(r, sb, datatype, dataspace, layout, pipeline)
}

// datasetMessages are the raw messages a dataset descriptor is built from.
type datasetMessages struct {
	datatype, dataspace, layout, pipeline *HeaderMessage
}

// findDatasetMessages picks the first message of each dataset type and
// requires the datatype, dataspace and layout; the filter pipeline is optional.
func findDatasetMessages(header *ObjectHeader) (*datasetMessages, error) {
	m := &datasetMessages{}

	for _, msg := range header.Messages {
		switch msg.Type {
		case MsgDatatype:
			m.datatype = firstMessage(m.datatype, msg)
		case MsgDataspace:
			m.dataspace = firstMessage(m.dataspace, msg)
		case MsgDataLayout:
			m.layout = firstMessage(m.layout, msg)
		case MsgFilterPipeline:
			m.pipeline = firstMessage(m.pipeline, msg)
		}
	}

	switch {
	case m.datatype == nil:
		return nil, fmt.Errorf("%w: datatype message not found", utils.ErrCorrupt)
	case m.dataspace == nil:
		return nil, fmt.Errorf("%w: dataspace message not found", utils.ErrCorrupt)
	case m.layout == nil:
		return nil, fmt.Errorf("%w: data layout message not found", utils.ErrCorrupt)
	}

	return m, nil
}

// firstMessage keeps the message already found, so the first occurrence of a
// type wins.
func firstMessage(found, msg *HeaderMessage) *HeaderMessage {
	if found != nil {
		return found
	}

	return msg
}

// newDatasetDescriptor validates parsed messages and builds the descriptor. It
// is separate from NewDatasetDescriptor so tests can exercise the validation on
// hand-built messages.
func newDatasetDescriptor(
	r io.ReaderAt,
	sb *Superblock,
	datatype *DatatypeMessage,
	dataspace *DataspaceMessage,
	layout *DataLayoutMessage,
	pipeline *FilterPipelineMessage,
) (*DatasetDescriptor, error) {
	if datatype == nil || dataspace == nil || layout == nil {
		return nil, fmt.Errorf("%w: dataset descriptor needs datatype, dataspace and layout", utils.ErrCorrupt)
	}

	// A zero-sized datatype cannot describe storage; C rejects it before any
	// size arithmetic (H5Dcontig.c:388, H5Dcompact.c:253).
	if datatype.Size == 0 {
		return nil, fmt.Errorf("%w: datatype size is zero", utils.ErrCorrupt)
	}

	nelmts, err := dataspaceElements(dataspace)
	if err != nil {
		return nil, err
	}

	elemSize := uint64(datatype.Size)

	// nelmts × dt_size, overflow-checked as C does by division
	// (H5Dcontig.c:392-396, H5Dcompact.c:262-266).
	dataBytes, err := utils.SafeMultiply(nelmts, elemSize)
	if err != nil {
		return nil, fmt.Errorf("%w: dataset size overflow: %d elements of %d bytes (size of dataset's storage overflowed)",
			utils.ErrCorrupt, nelmts, elemSize)
	}

	d := &DatasetDescriptor{
		datatype:       datatype,
		dataspace:      dataspace,
		layout:         layout,
		filterPipeline: pipeline,
		nelmts:         nelmts,
		elemSize:       elemSize,
		dataBytes:      dataBytes,
	}

	switch layout.Class {
	case LayoutContiguous:
		err = d.validateContiguous(r, sb)
	case LayoutCompact:
		err = d.validateCompact()
	case LayoutChunked:
		if err = d.validateChunked(); err != nil {
			err = fmt.Errorf("chunked layout (index at 0x%X): %w", layout.DataAddress, err)
		}
	case LayoutVirtual:
		err = fmt.Errorf("virtual dataset layout is not supported")
	default:
		err = fmt.Errorf("%w: unknown layout class %d", utils.ErrCorrupt, layout.Class)
	}

	if err != nil {
		return nil, err
	}

	return d, nil
}

// dataspaceElements returns the number of elements a dataspace describes, the
// product of its dimensions checked for overflow. DataspaceMessage.TotalElements
// multiplies without a check, and a wrapped product would make every later
// bound meaningless.
func dataspaceElements(ds *DataspaceMessage) (uint64, error) {
	switch ds.Type {
	case DataspaceNull:
		return 0, nil
	case DataspaceScalar:
		return 1, nil
	case DataspaceSimple:
	default:
		return 0, fmt.Errorf("%w: unknown dataspace type %d", utils.ErrCorrupt, ds.Type)
	}

	total := uint64(1)
	for i, dim := range ds.Dimensions {
		var err error

		total, err = utils.SafeMultiply(total, dim)
		if err != nil {
			return 0, fmt.Errorf("%w: dataspace element count overflows at dimension %d", utils.ErrCorrupt, i)
		}
	}

	return total, nil
}

// validateContiguous mirrors H5D__contig_check (H5Dcontig.c:369-416): the
// storage must lie inside the file. The region is checked only when the
// address is defined; an unallocated dataset has no storage yet (C reads the
// fill value, H5Dcontig.c:399).
func (d *DatasetDescriptor) validateContiguous(r io.ReaderAt, sb *Superblock) error {
	if d.layout.DataAddress == undefinedAddress {
		return nil
	}

	if _, err := contigDataSize(r, sb, d.layout.DataAddress, d.nelmts, d.elemSize, "contiguous dataset storage"); err != nil {
		return fmt.Errorf("%w: %w", utils.ErrCorrupt, err)
	}

	return nil
}

// validateCompact mirrors H5D__compact_init (H5Dcompact.c:253-270): the data
// stored in the layout message must be exactly nelmts × dt_size bytes. A
// mismatch would let a reader index past the buffer or return short data.
func (d *DatasetDescriptor) validateCompact() error {
	stored := uint64(len(d.layout.CompactData))
	if stored != d.dataBytes {
		return fmt.Errorf("%w: size of compact dataset's data buffer (%d bytes) doesn't match size of dataset data (%d bytes)",
			utils.ErrCorrupt, stored, d.dataBytes)
	}

	return nil
}

// validateChunked mirrors H5D__chunk_set_sizes and H5D__chunk_init:
//   - the chunk grid has one dimension more than the dataspace, the last being
//     the element size in bytes (H5Dchunk.c:909-913 writes it so), and that last
//     dimension must equal the datatype size (H5Dchunk.c:827-831);
//   - every chunk dimension is positive (H5Dchunk.c:1159, also checked when the
//     layout message is parsed);
//   - the chunk's byte size is the product of all dimensions, computed in 64
//     bits with an overflow check (H5Dchunk.c:861-873);
//   - with a version 1 B-tree index a chunk must be below 4 GB
//     (H5Dchunk.c:1196-1199).
//
// Deviation: the chunk size is also capped at utils.MaxChunkSize (1 GB), the
// ADR-003 bound. A reader allocates a whole chunk at once, and Go cannot
// recover from a failed allocation the way C recovers from malloc returning
// NULL.
func (d *DatasetDescriptor) validateChunked() error {
	rank := len(d.dataspace.Dimensions)
	dims := d.layout.ChunkSize

	if d.dataspace.Type != DataspaceSimple || rank == 0 {
		return fmt.Errorf("%w: chunked layout needs a simple dataspace of rank 1 or more", utils.ErrCorrupt)
	}

	if len(dims) != rank+1 {
		return fmt.Errorf("%w: chunked layout has %d dimensions, the dataspace rank %d needs %d",
			utils.ErrCorrupt, len(dims), rank, rank+1)
	}

	if dims[rank] != d.elemSize {
		return fmt.Errorf("%w: stored datatype size in chunk layout (%d) does not match datatype description (%d)",
			utils.ErrCorrupt, dims[rank], d.elemSize)
	}

	chunkBytes := uint64(1)
	for i, dim := range dims {
		if dim == 0 {
			return fmt.Errorf("%w: chunk dimension %d is zero", utils.ErrCorrupt, i)
		}

		var err error

		chunkBytes, err = utils.SafeMultiply(chunkBytes, dim)
		if err != nil {
			return fmt.Errorf("%w: chunk size overflows at dimension %d", utils.ErrCorrupt, i)
		}
	}

	if d.layout.ChunkIndexType == ChunkIndexBTREE && chunkBytes > maxBTreeChunkBytes {
		return fmt.Errorf("%w: chunk size %d must be < 4GB with v1 b-tree index", utils.ErrCorrupt, chunkBytes)
	}

	if chunkBytes > utils.MaxChunkSize {
		return fmt.Errorf("%w: chunk size %d exceeds the maximum %d", utils.ErrCorrupt, chunkBytes, utils.MaxChunkSize)
	}

	d.chunkBytes = chunkBytes

	return nil
}

// Datatype returns the dataset's parsed datatype message.
func (d *DatasetDescriptor) Datatype() *DatatypeMessage { return d.datatype }

// Dataspace returns the dataset's parsed dataspace message.
func (d *DatasetDescriptor) Dataspace() *DataspaceMessage { return d.dataspace }

// Layout returns the dataset's parsed data layout message.
func (d *DatasetDescriptor) Layout() *DataLayoutMessage { return d.layout }

// FilterPipeline returns the dataset's filter pipeline, or nil when it has none.
func (d *DatasetDescriptor) FilterPipeline() *FilterPipelineMessage { return d.filterPipeline }

// Dims returns the dataspace dimensions (empty for scalar and null dataspaces).
func (d *DatasetDescriptor) Dims() []uint64 { return d.dataspace.Dimensions }

// ChunkDims returns the chunk dimensions without the trailing element-size
// dimension, or nil for a dataset that is not chunked.
func (d *DatasetDescriptor) ChunkDims() []uint64 {
	if d.layout.Class != LayoutChunked {
		return nil
	}

	return d.layout.ChunkSize[:len(d.dataspace.Dimensions)]
}

// StorageSize returns nelmts × element size in bytes. For contiguous and
// compact layouts this is the storage the descriptor validated in the file.
func (d *DatasetDescriptor) StorageSize() uint64 { return d.dataBytes }

// ChunkGrid returns the number of chunks along each dataspace dimension,
// ceil(dims[i] / chunkDims[i]), or nil for a dataset that is not chunked.
func (d *DatasetDescriptor) ChunkGrid() []uint64 {
	if d.layout.Class != LayoutChunked {
		return nil
	}

	dims := d.dataspace.Dimensions
	grid := make([]uint64, len(dims))
	for i, dim := range dims {
		c := d.layout.ChunkSize[i]
		grid[i] = dim / c
		if dim%c != 0 {
			grid[i]++
		}
	}

	return grid
}

// CollectChunks returns the entries of the dataset's version 1 chunk B-tree,
// validated against the chunk grid:
//   - the walk checks node levels and refuses to visit a node twice
//     (BTreeV1Node.collectChunks);
//   - the entry count is at most the number of chunks in the grid, and at most
//     what the file can hold (each entry occupies its own key and child
//     pointer on disk);
//   - every key lies inside the grid, and the element-size coordinate is 0:
//     the key holds the chunk's first element (H5Dbtree.c:646 checks the
//     alignment while decoding).
//
// Violations wrap utils.ErrCorrupt.
func (d *DatasetDescriptor) CollectChunks(r io.ReaderAt, sb *Superblock) ([]ChunkEntry, error) {
	layout := d.layout
	if layout.Class != LayoutChunked || layout.ChunkIndexType != ChunkIndexBTREE {
		return nil, fmt.Errorf("unsupported chunk index type %d: only the v1 B-tree index is supported",
			layout.ChunkIndexType)
	}

	ndims := len(layout.ChunkSize)
	root, err := ParseBTreeV1Node(r, layout.DataAddress, sb.OffsetSize, ndims, layout.ChunkSize)
	if err != nil {
		return nil, fmt.Errorf("failed to parse chunk B-tree: %w", err)
	}

	grid := d.ChunkGrid()
	maxEntries := uint64(1)
	for _, g := range grid {
		maxEntries, err = utils.SafeMultiply(maxEntries, g)
		if err != nil {
			maxEntries = math.MaxUint64
			break
		}
	}

	if limit := dataLimit(r, sb); limit > 0 {
		entryBytes := 8 + 8*uint64(len(layout.ChunkSize)) + uint64(sb.OffsetSize) // Key (nbytes, mask, coordinates) and child.
		maxEntries = min(maxEntries, limit/entryBytes)
	}

	chunks, err := root.collectChunks(r, sb.OffsetSize, layout.ChunkSize, maxEntries)
	if err != nil {
		return nil, err
	}

	rank := len(grid)
	for _, c := range chunks {
		for i, g := range grid {
			if c.Key.Scaled[i] >= g {
				return nil, fmt.Errorf("%w: chunk at 0x%X has coordinate %d in dimension %d, the grid has %d chunks",
					utils.ErrCorrupt, c.Address, c.Key.Scaled[i], i, g)
			}
		}
		if c.Key.Scaled[rank] != 0 {
			return nil, fmt.Errorf("%w: chunk at 0x%X has a nonzero element-size coordinate", utils.ErrCorrupt, c.Address)
		}
	}

	return chunks, nil
}

// ChunkBytes returns the byte size of one chunk (the product of the chunk
// dimensions, element size included), or 0 for a dataset that is not chunked.
// A valid filter pipeline decodes every chunk to exactly this size.
func (d *DatasetDescriptor) ChunkBytes() uint64 { return d.chunkBytes }

// NumElements returns the number of elements in the dataspace.
func (d *DatasetDescriptor) NumElements() uint64 { return d.nelmts }

// ElementSize returns the datatype size in bytes; it is never zero.
func (d *DatasetDescriptor) ElementSize() uint64 { return d.elemSize }

// ResultSize returns the byte size of the raw buffer holding nelmts elements of
// this dataset. It is the one place a dataset reader sizes a result from file
// metadata (ADR-004 §1); a guard test rejects any other allocation sized from
// an element size.
//
// resultElemSize is the in-memory cost of one element of what the reader
// returns (ResultFloat64 for []float64, CompoundResultSize for compound maps,
// …). A read allocates both the raw buffer and that result, so both are
// bounded:
//   - chunked data can legitimately be larger than the file, because unwritten
//     chunks read as the fill value (H5Dchunk.c:3041-3046), so no file-derived
//     bound is correct: the larger of nelmts × elemSize and
//     nelmts × resultElemSize must fit the caller's read limit (ErrLimit);
//   - contiguous and compact data exists in the file, and the descriptor
//     validated its size against it. A conversion of up to maxFreeConversion
//     times the measured bytes (Read's []float64 of a uint8 dataset) is
//     accepted as bounded by the file; a larger result (compound maps cost tens
//     of times their raw bytes) must fit the read limit as well.
//
// Both sizes must also fit in int on this platform; on a 32-bit target that is
// what stops a []float64 larger than the address space (ErrLimit).
//
// C never sizes the result of H5Dread, the caller passes the buffer; this
// library allocates on the caller's behalf and a failed Go allocation cannot be
// recovered, which is why the limit exists (ADR-003, ADR-004).
func (d *DatasetDescriptor) ResultSize(nelmts, resultElemSize, limit uint64) (int, error) {
	if nelmts > d.nelmts {
		return 0, fmt.Errorf("selection of %d elements exceeds the dataset's %d elements", nelmts, d.nelmts)
	}

	// No overflow: nelmts ≤ d.nelmts, and d.nelmts × elemSize was checked when
	// the descriptor was built.
	size := nelmts * d.elemSize

	// Unallocated contiguous storage was not checked against the file
	// (H5Dcontig.c:399), so it bounds nothing. C reads the fill value there;
	// this library does not support fill values yet, so the read is refused
	// before anything is allocated.
	if d.layout.Class == LayoutContiguous && d.layout.DataAddress == undefinedAddress && size > 0 {
		return 0, fmt.Errorf("contiguous dataset storage is not allocated (undefined address)")
	}

	// A product that overflows is certainly over any limit.
	converted, err := utils.SafeMultiply(nelmts, resultElemSize)
	if err != nil {
		converted = math.MaxUint64
	}
	needed := max(size, converted)

	limited := d.layout.Class == LayoutChunked || converted/maxFreeConversion > size
	if limited && needed > limit {
		return 0, fmt.Errorf("%w: reading %d elements needs %d bytes, more than the read limit of %d bytes; "+
			"raise it with (*hdf5.File).SetReadLimit", utils.ErrLimit, nelmts, needed, limit)
	}

	if needed > uint64(math.MaxInt) {
		return 0, fmt.Errorf("%w: reading %d elements needs %d bytes, more than this platform can address",
			utils.ErrLimit, nelmts, needed)
	}

	return int(size), nil //nolint:gosec // G115: size ≤ needed ≤ math.MaxInt, checked above.
}

// maxFreeConversion is the largest result-to-storage ratio accepted for data
// that exists in the file without consulting the read limit: Read's 8-byte
// float64 per 1-byte integer.
const maxFreeConversion = 8

// In-memory cost of one element of each reader's result, for ResultSize. They
// count the slice element; string and byte-slice contents are copies of raw
// bytes the raw size already counts. Heap-referenced (variable-length) data is
// counted as it is dereferenced (heapReader).
const (
	ResultFloat64 uint64 = 8  // float64.
	ResultString  uint64 = 16 // String header.
	ResultBytes   uint64 = 24 // []byte header.
)

// Cost model of one compound element (CompoundValue, a map[string]any),
// measured with runtime.MemStats.TotalAlloc on 20,000-element reads with Go
// 1.27 (TASK-062 review): 352 B for one float64 member, 408 B for eight,
// 2,273 B for sixteen, 4,736 B for thirty-two (about 148 B per member once
// the map outgrows its first group of eight); a 16-byte string member adds its
// bytes plus a 16-byte header. The constants are upper bounds of those
// measurements, so the estimate never undercounts.
const (
	compoundMapBase  uint64 = 384 // Slice element, map header and first group.
	compoundPerEntry uint64 = 160 // Key, slot share and growth per member.
	compoundBoxed    uint64 = 8   // A numeric value boxed in an interface.
	compoundStrHead  uint64 = 16  // String header of a string member.
)

// CompoundResultSize returns the in-memory cost of one decoded element of
// compound type ct: the map, one entry per member, and each member's value,
// recursively for nested compounds. Variable-length members count only their
// headers here; their heap bytes are counted when they are dereferenced.
func CompoundResultSize(ct *CompoundType) uint64 {
	total := compoundMapBase
	for _, m := range ct.Members {
		total += compoundPerEntry + memberResultSize(m.Type)
	}

	return total
}

// memberResultSize is the cost of one decoded member value.
func memberResultSize(dt *DatatypeMessage) uint64 {
	switch {
	case dt == nil:
		return compoundBoxed
	case dt.IsCompound():
		nested, err := ParseCompoundType(dt)
		if err != nil {
			return compoundMapBase // Decoding will fail on the same error.
		}
		return CompoundResultSize(nested)
	case dt.IsFixedString():
		return compoundStrHead + uint64(dt.Size)
	case dt.IsVariableString():
		return compoundStrHead
	default:
		return compoundBoxed
	}
}
