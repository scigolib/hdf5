package core

import (
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/scigolib/hdf5/internal/utils"
)

// ReadDatasetFloat64 reads a dataset and returns values as float64 array.
// It builds the dataset's descriptor from the object header and applies the
// default read limit; callers that hold a descriptor and a configured limit use
// ReadFloat64.
func ReadDatasetFloat64(r io.ReaderAt, header *ObjectHeader, sb *Superblock) ([]float64, error) {
	desc, err := NewDatasetDescriptor(r, header, sb)
	if err != nil {
		return nil, err
	}

	return ReadFloat64(r, sb, desc, utils.DefaultReadLimit)
}

// ReadFloat64 reads every element of the dataset desc describes and returns the
// values as float64. limit bounds the bytes a chunked read may allocate
// (DatasetDescriptor.ResultSize).
func ReadFloat64(r io.ReaderAt, sb *Superblock, desc *DatasetDescriptor, limit uint64) ([]float64, error) {
	if desc.nelmts == 0 {
		return []float64{}, nil
	}

	// Refuse an unconvertible datatype before the storage is read: a fill
	// value can stand for storage of any size up to the read limit.
	if err := CheckFloat64Convertible(desc.datatype); err != nil {
		return nil, err
	}

	resultElemSize, err := Float64ResultSize(desc.datatype)
	if err != nil {
		return nil, err
	}

	rawData, err := readRawData(r, sb, desc, resultElemSize, limit)
	if err != nil {
		return nil, err
	}

	return ConvertToFloat64(rawData, desc.datatype, desc.nelmts)
}

// flattenArray returns the type each value of a dataset element is decoded
// from and how many values one element holds: the datatype itself and one, or
// for an array (nested arrays included) the innermost base type and the
// product of all array dimensions. C converts an array element as its nelem
// base values in order (H5T__conv_array, H5Tconv_array.c:195-199), so the
// values come out flattened in C order with the array dimensions innermost,
// as h5dump prints them.
//
// The size of an array is always nelem × the base size: H5T__array_create sets
// it (H5Tarray.c:162) and H5T_set_size refuses an array (H5T.c:3049). The
// decoder takes the size from the file without that check, so a type whose
// size disagrees is damaged and is refused here before any value is decoded.
func flattenArray(dt *DatatypeMessage) (*DatatypeMessage, uint64, error) {
	count := uint64(1)
	for dt.Class == DatatypeArray {
		if dt.Base == nil {
			return nil, 0, fmt.Errorf("%w: array datatype without a base type", utils.ErrCorrupt)
		}
		n := uint64(1)
		for _, d := range dt.ArrayDims {
			var err error
			if n, err = utils.SafeMultiply(n, uint64(d)); err != nil {
				return nil, 0, fmt.Errorf("%w: array datatype element count: %w", utils.ErrCorrupt, err)
			}
		}
		need, err := utils.SafeMultiply(n, uint64(dt.Base.Size))
		if err != nil || need != uint64(dt.Size) {
			return nil, 0, fmt.Errorf("%w: array datatype of %d %d-byte elements has size %d "+
				"(H5Tarray.c:162 makes it the product)", utils.ErrCorrupt, n, dt.Base.Size, dt.Size)
		}
		// No overflow: count × n is at most the outermost size, a uint32.
		count *= n
		dt = dt.Base
	}

	return dt, count, nil
}

// Float64ResultSize returns the in-memory cost of one dataset element in a
// float64 read, for ResultSize: one float64, or one per value of an array
// element (flattenArray).
func Float64ResultSize(datatype *DatatypeMessage) (uint64, error) {
	_, count, err := flattenArray(datatype)
	if err != nil {
		return 0, err
	}

	// No overflow: count is at most the array's 32-bit size.
	return ResultFloat64 * count, nil
}

// readRawData returns the stored bytes of every element of the dataset, for
// any supported layout. Contiguous and compact storage was validated when the
// descriptor was built; chunked storage is sized under the read limit.
func readRawData(r io.ReaderAt, sb *Superblock, desc *DatasetDescriptor, resultElemSize, limit uint64) ([]byte, error) {
	switch desc.layout.Class {
	case LayoutCompact:
		// Validated to hold exactly nelmts × elemSize bytes (H5D__compact_init).
		return desc.layout.CompactData, nil

	case LayoutContiguous:
		rawData, err := readContiguousDatasetData(r, sb, desc, resultElemSize, limit)
		if err != nil {
			return nil, fmt.Errorf("failed to read contiguous data: %w", err)
		}
		return rawData, nil

	case LayoutChunked:
		rawData, err := readChunkedData(r, sb, desc, resultElemSize, limit)
		if err != nil {
			return nil, fmt.Errorf("failed to read chunked data: %w", err)
		}
		return rawData, nil

	default:
		return nil, fmt.Errorf("unsupported layout class: %d", desc.layout.Class)
	}
}

// ConvertToFloat64 converts raw element bytes to a float64 slice based on
// the datatype. Exported so the hyperslab reader shares the exact same
// datatype coverage as the whole-dataset Read() path (notably fixed-point
// integers of every width/sign — int16, uint8, …), instead of maintaining
// a second, narrower converter that silently rejected those types.
//
// An array element converts to its base values, flattened (flattenArray), so
// the result holds numElements × the array's value count numbers.
func ConvertToFloat64(rawData []byte, datatype *DatatypeMessage, numElements uint64) ([]float64, error) {
	base, count, err := flattenArray(datatype)
	if err != nil {
		return nil, err
	}

	values, err := utils.SafeMultiply(numElements, count)
	if err != nil {
		return nil, fmt.Errorf("data size overflow (convert to float64): %w", err)
	}

	return convertToFloat64(rawData, base, values)
}

// convertToFloat64 converts raw bytes to float64 array based on datatype.
func convertToFloat64(rawData []byte, datatype *DatatypeMessage, numElements uint64) ([]float64, error) {
	datatype = enumAsBase(datatype)

	// ADR-003 (issue #68): numElements comes from the dataspace, i.e. from file
	// metadata, so the result buffer sized from it is validated before it is
	// allocated — a failed Go allocation is a fatal runtime error, unlike the C
	// library's recoverable NULL malloc. The support check comes first so an
	// unsupported datatype keeps its own error instead of a size mismatch; it
	// also guarantees that every path reaching the allocation is one where
	// datatype.Size is the converter's element width and the guard below is
	// therefore meaningful.
	if err := CheckFloat64Convertible(datatype); err != nil {
		return nil, err
	}

	required, err := utils.SafeMultiply(numElements, uint64(datatype.Size))
	if err != nil {
		return nil, fmt.Errorf("data size overflow (convert to float64): %w", err)
	}

	if required > uint64(len(rawData)) {
		return nil, fmt.Errorf("data truncated: need %d bytes for %d elements, have %d",
			required, numElements, len(rawData))
	}

	result := make([]float64, numElements)
	byteOrder := datatype.GetByteOrder()

	switch {
	case datatype.IsFloat64():
		// IEEE 754 double precision (64-bit).
		for i := uint64(0); i < numElements; i++ {
			offset := i * 8
			if offset+8 > uint64(len(rawData)) {
				return nil, errors.New("data truncated (float64)")
			}

			bits := byteOrder.Uint64(rawData[offset : offset+8])
			result[i] = math.Float64frombits(bits)
		}

	case datatype.IsFloat32():
		// IEEE 754 single precision (32-bit).
		for i := uint64(0); i < numElements; i++ {
			offset := i * 4
			if offset+4 > uint64(len(rawData)) {
				return nil, errors.New("data truncated (float32)")
			}

			bits := byteOrder.Uint32(rawData[offset : offset+4])
			result[i] = float64(math.Float32frombits(bits))
		}

	case datatype.IsFixedPoint():
		// Fixed-point integer of any width (1/2/4/8 bytes), signed or
		// unsigned. The HDF5 spec encodes width in datatype.Size and
		// signedness in bit 3 of ClassBitField. The branching is on
		// Size first because the byte read is the same regardless of
		// signedness; signedness only changes the reinterpretation.
		signed := datatype.IsSignedFixedPoint()
		switch datatype.Size {
		case 1:
			if numElements > uint64(len(rawData)) {
				return nil, errors.New("data truncated (1-byte int)")
			}
			if signed {
				for i := uint64(0); i < numElements; i++ {
					//nolint:gosec // G115: spec-mandated uint8→int8 reinterpretation
					result[i] = float64(int8(rawData[i]))
				}
			} else {
				for i := uint64(0); i < numElements; i++ {
					result[i] = float64(rawData[i])
				}
			}
		case 2:
			if numElements*2 > uint64(len(rawData)) {
				return nil, errors.New("data truncated (2-byte int)")
			}
			if signed {
				for i := uint64(0); i < numElements; i++ {
					//nolint:gosec // G115: spec-mandated uint16→int16 reinterpretation
					result[i] = float64(int16(byteOrder.Uint16(rawData[i*2 : i*2+2])))
				}
			} else {
				for i := uint64(0); i < numElements; i++ {
					result[i] = float64(byteOrder.Uint16(rawData[i*2 : i*2+2]))
				}
			}
		case 4:
			if numElements*4 > uint64(len(rawData)) {
				return nil, errors.New("data truncated (4-byte int)")
			}
			if signed {
				for i := uint64(0); i < numElements; i++ {
					//nolint:gosec // G115: spec-mandated uint32→int32 reinterpretation
					result[i] = float64(int32(byteOrder.Uint32(rawData[i*4 : i*4+4])))
				}
			} else {
				for i := uint64(0); i < numElements; i++ {
					result[i] = float64(byteOrder.Uint32(rawData[i*4 : i*4+4]))
				}
			}
		case 8:
			if numElements*8 > uint64(len(rawData)) {
				return nil, errors.New("data truncated (8-byte int)")
			}
			if signed {
				for i := uint64(0); i < numElements; i++ {
					//nolint:gosec // G115: spec-mandated uint64→int64 reinterpretation
					result[i] = float64(int64(byteOrder.Uint64(rawData[i*8 : i*8+8])))
				}
			} else {
				for i := uint64(0); i < numElements; i++ {
					// uint64 above 2^53 loses precision in float64; this
					// matches the existing int64 path's behavior and is
					// what the public Read API (returning []float64) can
					// represent.
					result[i] = float64(byteOrder.Uint64(rawData[i*8 : i*8+8]))
				}
			}
		default:
			return nil, fmt.Errorf("unsupported fixed-point width %d bytes: %w", datatype.Size, utils.ErrNotSupported)
		}

	default:
		return nil, fmt.Errorf("unsupported datatype for conversion to float64: %s: %w", datatype, utils.ErrNotSupported)
	}

	return result, nil
}

// CheckFloat64Convertible returns an error wrapping utils.ErrNotSupported when
// the datatype has no float64 conversion, so a reader can refuse it before it
// reads or fills any storage. An array is convertible when its base is; an
// array whose size disagrees with its dimensions wraps utils.ErrCorrupt.
func CheckFloat64Convertible(datatype *DatatypeMessage) error {
	base, _, err := flattenArray(datatype)
	if err != nil {
		return err
	}
	if !isFloat64Convertible(base) {
		return fmt.Errorf("unsupported datatype for conversion to float64: %s: %w", datatype, utils.ErrNotSupported)
	}
	return nil
}

// isFloat64Convertible reports whether convertToFloat64 can convert the
// datatype. It mirrors the switch in convertToFloat64 so the allocation guard
// runs only where Size is the converter's element width (1/2/4/8-byte
// fixed-point, 4/8-byte float).
func isFloat64Convertible(datatype *DatatypeMessage) bool {
	datatype = enumAsBase(datatype)
	if datatype.IsFloat64() || datatype.IsFloat32() {
		return true
	}

	if !datatype.IsFixedPoint() {
		return false
	}

	switch datatype.Size {
	case 1, 2, 4, 8:
		return true
	default:
		return false
	}
}

// enumAsBase returns the integer base type of an enumeration, so that a
// numeric read returns each element's integer value, and dt itself otherwise.
//
// This is the C library's enum-to-numeric conversion: H5T__conv_enum_numeric
// (H5Tconv_enum.c:507-561, registered for integer and float destinations at
// H5T.c:1422-1423) converts the values through the enumeration's parent type,
// so H5Dread into H5T_NATIVE_DOUBLE yields the base integer values; h5py reads
// enums the same way. The codec keeps the member names and values for an
// enum-aware API. The decoder guarantees the base has the enum's size (datatype_codec.go
// decodeEnum), which the size check below re-asserts for hand-built types.
func enumAsBase(dt *DatatypeMessage) *DatatypeMessage {
	if dt.Class == DatatypeEnum && dt.Base != nil && dt.Base.IsFixedPoint() && dt.Base.Size == dt.Size {
		return dt.Base
	}
	return dt
}

// validateElementStorage rejects an element count that cannot fit in the bytes
// read from the file. Both the count and the element size come from file
// metadata, and a compact or empty layout can carry a huge count with no data
// at all: sizing the result slice from that count reaches make() with a
// terabyte request. A failed allocation of that size is a fatal runtime throw
// that no caller can recover, and where the mapping succeeds the call blocks
// inside the runtime's allocation path; either way a caller-level timeout cannot
// help (found by FuzzOpenFile: make([]CompoundValue, 3.3e12) from a corrupted
// dataspace).
func validateElementStorage(numElements, elemSize uint64, data []byte, what string) error {
	if elemSize == 0 {
		return fmt.Errorf("%s: element size is zero", what)
	}

	required, err := utils.SafeMultiply(numElements, elemSize)
	if err != nil {
		return fmt.Errorf("%s: size overflow: %w", what, err)
	}

	if required > uint64(len(data)) {
		return fmt.Errorf("%s: data size mismatch: need %d bytes for %d elements, have %d",
			what, required, numElements, len(data))
	}

	return nil
}

// readChunkedData reads every chunk of a chunked dataset into one buffer.
//
// The buffer is sized by DatasetDescriptor.ResultSize under the read limit: a
// chunked dataset may legitimately be larger than the file (unwritten chunks
// read as the fill value, H5Dchunk.c:3041-3046), so no file-derived bound is
// correct. This replaces the former 1 TB cap, which let a few-KB file demand a
// terabyte (ADR-004 §1). Each chunk is read through ReadBlock, bounded by the
// file and by the ADR-003 chunk cap.
func readChunkedData(r io.ReaderAt, sb *Superblock, desc *DatasetDescriptor, resultElemSize, limit uint64) ([]byte, error) {
	totalBytes, err := desc.ResultSize(desc.nelmts, resultElemSize, limit)
	if err != nil {
		return nil, err
	}

	rawData := make([]byte, totalBytes)
	dataDims := desc.dataspace.Dimensions
	chunkDims := desc.ChunkDims()

	// Chunks never written read as the fill value (H5Dchunk.c:2936-2949,
	// 3041-3046). One chunk of fill is built on the first missing chunk; a
	// zero fill leaves the zeroed result as it is.
	var fillChunk []byte
	var fillZero bool

	// The index walk is bounded and every entry checked against the chunk grid
	// (DatasetDescriptor.VisitChunks); its bookkeeping, at most one bit per
	// chunk and so at most an eighth of the result, shares the read limit.
	err = desc.VisitChunks(r, sb, nil, limit-uint64(totalBytes), ChunkVisitor{ //nolint:gosec // G115: a result size is never negative.
		Stored: func(chunk *ChunkEntry) error {
			chunkData, err := desc.ReadChunk(r, sb, chunk)
			if err != nil {
				return err
			}

			return copyChunkToArray(chunkData, rawData, chunk.Key.Scaled[:len(dataDims)], chunkDims, dataDims, desc.elemSize)
		},
		Missing: func(scaled []uint64) error {
			if fillChunk == nil {
				var err error
				if fillChunk, fillZero, err = desc.FillChunk(); err != nil {
					return err
				}
			}
			if fillZero {
				return nil
			}

			return copyChunkToArray(fillChunk, rawData, scaled, chunkDims, dataDims, desc.elemSize)
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to read chunks: %w", err)
	}

	return rawData, nil
}

// ReadChunk reads one stored chunk and decodes it through the filter
// pipeline. The stored size comes from the index, i.e. file metadata: the
// block funnel bounds it by the file and by the chunk cap (CVE-2025-7067,
// ADR-003), and no filter may produce more than the chunk's byte size.
func (d *DatasetDescriptor) ReadChunk(r io.ReaderAt, sb *Superblock, chunk *ChunkEntry) ([]byte, error) {
	chunkData, err := ReadBlock(r, sb, chunk.Address, uint64(chunk.Key.Nbytes), utils.MaxChunkSize, "chunk data")
	if err != nil {
		return nil, fmt.Errorf("failed to read chunk at 0x%x: %w", chunk.Address, err)
	}

	if d.filterPipeline != nil {
		chunkData, err = d.filterPipeline.ApplyFiltersMasked(chunkData, chunk.Key.FilterMask, d.chunkBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to apply filters to chunk at 0x%x: %w", chunk.Address, err)
		}
	}

	return chunkData, nil
}

// copyChunkToArray copies chunk data to the correct position in full array.
// This handles multi-dimensional indexing and partial chunks at boundaries.
func copyChunkToArray(chunkData, fullData []byte, chunkCoords, chunkSize, dataDims []uint64, elemSize uint64) error {
	ndims := len(chunkCoords)
	if ndims != len(chunkSize) || ndims != len(dataDims) {
		return errors.New("dimension mismatch")
	}

	// Use general N-dimensional algorithm.
	return copyNDChunk(chunkData, fullData, chunkCoords, chunkSize, dataDims, elemSize)
}

// copyNDChunk copies an N-dimensional chunk to the full N-dimensional array.
// Uses general algorithm that works for any number of dimensions.
func copyNDChunk(chunkData, fullData []byte, chunkCoords, chunkSize, dataDims []uint64, elemSize uint64) error {
	ndims := len(chunkCoords)

	// Calculate strides for both chunk and full array.
	// Stride[i] = product of all dimensions after i.
	chunkStrides := make([]uint64, ndims)
	dataStrides := make([]uint64, ndims)

	chunkStrides[ndims-1] = 1
	dataStrides[ndims-1] = 1
	for i := ndims - 2; i >= 0; i-- {
		chunkStrides[i] = chunkStrides[i+1] * chunkSize[i+1]
		dataStrides[i] = dataStrides[i+1] * dataDims[i+1]
	}

	// Calculate actual dimensions to copy (may be less than chunk size at boundaries).
	copyDims := make([]uint64, ndims)
	for i := 0; i < ndims; i++ {
		// Starting position of this chunk in dataset.
		startPos := chunkCoords[i] * chunkSize[i]
		// Maximum elements we can copy in this dimension.
		maxCopy := chunkSize[i]
		if startPos+maxCopy > dataDims[i] {
			maxCopy = dataDims[i] - startPos
		}
		copyDims[i] = maxCopy
	}

	// Calculate starting offset in full array for this chunk.
	dataOffset := uint64(0)
	for i := 0; i < ndims; i++ {
		dataOffset += chunkCoords[i] * chunkSize[i] * dataStrides[i]
	}

	// Use recursive N-dimensional iteration to copy elements.
	indices := make([]uint64, ndims)
	return copyNDChunkRecursive(chunkData, fullData, indices, 0, copyDims, chunkStrides, dataStrides, dataOffset, elemSize)
}

// copyNDChunkRecursive recursively iterates through N-dimensional indices.
func copyNDChunkRecursive(chunkData, fullData []byte, indices []uint64, dim int, copyDims, chunkStrides, dataStrides []uint64, dataBaseOffset, elemSize uint64) error {
	ndims := len(indices)

	if dim == ndims-1 {
		// Base case: copy a contiguous row.
		numElements := copyDims[dim]

		// Calculate source offset in chunk.
		chunkOffset := uint64(0)
		for i := 0; i < ndims; i++ {
			chunkOffset += indices[i] * chunkStrides[i]
		}
		chunkOffset *= elemSize

		// Calculate destination offset in full array.
		dataOffset := dataBaseOffset
		for i := 0; i < ndims-1; i++ {
			dataOffset += indices[i] * dataStrides[i]
		}
		dataOffset *= elemSize

		numBytes := numElements * elemSize

		// Bounds check.
		if chunkOffset+numBytes > uint64(len(chunkData)) {
			return fmt.Errorf("chunk data truncated: need %d bytes at offset %d, have %d total",
				numBytes, chunkOffset, len(chunkData))
		}
		if dataOffset+numBytes > uint64(len(fullData)) {
			return fmt.Errorf("full data overflow: need %d bytes at offset %d, have %d total",
				numBytes, dataOffset, len(fullData))
		}

		// Copy the row.
		copy(fullData[dataOffset:dataOffset+numBytes], chunkData[chunkOffset:chunkOffset+numBytes])
		return nil
	}

	// Recursive case: iterate through this dimension.
	for indices[dim] = 0; indices[dim] < copyDims[dim]; indices[dim]++ {
		err := copyNDChunkRecursive(chunkData, fullData, indices, dim+1, copyDims, chunkStrides, dataStrides, dataBaseOffset, elemSize)
		if err != nil {
			return err
		}
	}

	return nil
}
