package core

import (
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/scigolib/hdf5/internal/utils"
)

// CompoundValue represents a single compound structure instance as a map of field names to values.
type CompoundValue map[string]interface{}

// ReadDatasetCompound reads a dataset with compound datatype and returns array
// of compound values. It builds the dataset's descriptor and applies the
// default read limit; callers that hold a descriptor use ReadCompound.
func ReadDatasetCompound(r io.ReaderAt, header *ObjectHeader, sb *Superblock) ([]CompoundValue, error) {
	desc, err := NewDatasetDescriptor(r, header, sb)
	if err != nil {
		return nil, err
	}

	return ReadCompound(r, sb, desc, utils.DefaultReadLimit)
}

// ReadCompound reads every element of a compound dataset. The decoded maps
// cost far more than their raw bytes, so their estimated size
// (CompoundResultSize) is checked against the read limit for every layout,
// and variable-length members are dereferenced under the same limit.
func ReadCompound(r io.ReaderAt, sb *Superblock, desc *DatasetDescriptor, limit uint64) ([]CompoundValue, error) {
	if !desc.datatype.IsCompound() {
		return nil, errors.New("not a compound datatype")
	}

	compoundType, err := ParseCompoundType(desc.datatype)
	if err != nil {
		return nil, fmt.Errorf("failed to parse compound type: %w", err)
	}

	if desc.nelmts == 0 {
		return []CompoundValue{}, nil
	}

	rawData, err := readRawData(r, sb, desc, CompoundResultSize(compoundType), limit)
	if err != nil {
		return nil, err
	}

	return parseCompoundDataWith(rawData, compoundType, desc.nelmts, newHeapReader(r, sb, limit))
}

// parseCompoundData parses raw bytes into array of compound values, with the
// default read limit for heap dereferences.
func parseCompoundData(rawData []byte, compoundType *CompoundType, numElements uint64, r io.ReaderAt, sb *Superblock) ([]CompoundValue, error) {
	return parseCompoundDataWith(rawData, compoundType, numElements, newHeapReader(r, sb, utils.DefaultReadLimit))
}

// parseCompoundDataWith parses raw bytes into compound values, dereferencing
// variable-length members through heap.
func parseCompoundDataWith(rawData []byte, compoundType *CompoundType, numElements uint64, heap *heapReader) ([]CompoundValue, error) {
	structSize := uint64(compoundType.Size)

	// The count comes from the dataspace and the element size from the
	// datatype; neither is bounded by the bytes actually read, and a compact or
	// empty layout supplies none (ADR-003; found by FuzzOpenFile).
	if err := validateElementStorage(numElements, structSize, rawData, "compound data"); err != nil {
		return nil, err
	}

	result := make([]CompoundValue, numElements)

	for i := uint64(0); i < numElements; i++ {
		structOffset := i * structSize

		if structOffset+structSize > uint64(len(rawData)) {
			return nil, fmt.Errorf("data truncated at element %d", i)
		}

		structData := rawData[structOffset : structOffset+structSize]
		value := make(CompoundValue)

		// Parse each member.
		for _, member := range compoundType.Members {
			// member.Offset comes from the compound datatype properties, i.e.
			// from file metadata. A value beyond the struct size used to panic
			// the slice expression below (found by TestCorruptionCorpus), so it
			// is validated first (ADR-003).
			if uint64(member.Offset) > structSize {
				return nil, fmt.Errorf("member %s offset %d exceeds compound size %d",
					member.Name, member.Offset, structSize)
			}

			memberData := structData[member.Offset:]

			memberValue, err := parseMemberValueWith(memberData, member.Type, heap)
			if err != nil {
				return nil, fmt.Errorf("failed to parse member %s: %w", member.Name, err)
			}

			value[member.Name] = memberValue
		}

		result[i] = value
	}

	return result, nil
}

// parseMemberValue parses a single member value based on its datatype, with the
// default read limit for heap dereferences.
func parseMemberValue(data []byte, datatype *DatatypeMessage, r io.ReaderAt, sb *Superblock) (interface{}, error) {
	return parseMemberValueWith(data, datatype, newHeapReader(r, sb, utils.DefaultReadLimit))
}

// parseMemberValueWith parses a single member value, dereferencing a
// variable-length string through heap.
func parseMemberValueWith(data []byte, datatype *DatatypeMessage, heap *heapReader) (interface{}, error) {
	byteOrder := datatype.GetByteOrder()

	switch {
	case datatype.IsFloat64():
		if len(data) < 8 {
			return nil, errors.New("insufficient data for float64")
		}
		bits := byteOrder.Uint64(data[0:8])
		return math.Float64frombits(bits), nil

	case datatype.IsFloat32():
		if len(data) < 4 {
			return nil, errors.New("insufficient data for float32")
		}
		bits := byteOrder.Uint32(data[0:4])
		return math.Float32frombits(bits), nil

	case datatype.IsInt32():
		if len(data) < 4 {
			return nil, errors.New("insufficient data for int32")
		}
		//nolint:gosec // G115: HDF5 binary format requires uint32 to int32 conversion
		return int32(byteOrder.Uint32(data[0:4])), nil

	case datatype.IsInt64():
		if len(data) < 8 {
			return nil, errors.New("insufficient data for int64")
		}
		//nolint:gosec // G115: HDF5 binary format requires uint64 to int64 conversion
		return int64(byteOrder.Uint64(data[0:8])), nil

	case datatype.IsFixedString():
		// CVE-2025-2926 fix: Validate string size before processing.
		stringSize := uint64(datatype.Size)
		if err := utils.ValidateBufferSize(stringSize, utils.MaxStringSize, "compound string field"); err != nil {
			return nil, fmt.Errorf("string field too large: %w", err)
		}

		//nolint:gosec // G115: Safe length comparison
		if uint32(len(data)) < datatype.Size {
			return nil, errors.New("insufficient data for string")
		}
		// Extract string based on padding type.
		str := extractString(data[0:datatype.Size], datatype.GetStringPadding())
		return str, nil

	case datatype.IsVariableString():
		// Variable-length strings are stored as global heap references.
		// Parse the reference: heap_address (offset_size bytes) + object_index (4 bytes).
		return heap.readString(data)

	case datatype.IsCompound():
		// Nested compound - recursive parse.
		nestedCompound, err := ParseCompoundType(datatype)
		if err != nil {
			return nil, fmt.Errorf("failed to parse nested compound: %w", err)
		}
		values, err := parseCompoundDataWith(data, nestedCompound, 1, heap)
		if err != nil {
			return nil, err
		}
		return values[0], nil

	default:
		return nil, fmt.Errorf("unsupported member datatype: %s", datatype)
	}
}

// extractString extracts a string from fixed-length byte array based on padding type.
func extractString(data []byte, paddingType uint8) string {
	switch paddingType {
	case 0: // Null-terminated.
		for i, b := range data {
			if b == 0 {
				return string(data[:i])
			}
		}
		return string(data)

	case 1: // Null-padded.
		end := len(data)
		for end > 0 && data[end-1] == 0 {
			end--
		}
		return string(data[:end])

	case 2: // Space-padded.
		end := len(data)
		for end > 0 && (data[end-1] == ' ' || data[end-1] == 0) {
			end--
		}
		return string(data[:end])

	default:
		return string(data)
	}
}

// readVariableString reads a variable-length string from the Global Heap, with
// the default read limit. The data contains a Global Heap reference:
// heap_address (offset_size bytes) + object_index (4 bytes).
func readVariableString(r io.ReaderAt, data []byte, sb *Superblock) (string, error) {
	return newHeapReader(r, sb, utils.DefaultReadLimit).readString(data)
}

// heapReader dereferences global heap references for one dataset read. It
// caches each collection once (the per-element re-read made a dataset of
// references to one large object read the whole collection per element) and
// counts two amounts against the read limit, for every layout:
//   - the bytes of collections it holds in its cache, because many overlapping
//     collections can each cover most of the file;
//   - the bytes it hands out, because every element may reference the same
//     large object: 65,536 references (1 MB of raw data) to a 1 MB object would
//     otherwise return 64 GiB from a 2 MB file (review finding H3).
//
// Exceeding either wraps utils.ErrLimit. Values are copies, so callers may
// modify them; the copies are what is counted.
type heapReader struct {
	r     io.ReaderAt
	sb    *Superblock
	limit uint64

	cache       map[uint64]*GlobalHeapCollection
	cachedBytes uint64
	handedOut   uint64
}

func newHeapReader(r io.ReaderAt, sb *Superblock, limit uint64) *heapReader {
	return &heapReader{r: r, sb: sb, limit: limit, cache: make(map[uint64]*GlobalHeapCollection)}
}

// object returns the data of object index in the collection at addr, without
// copying, and counts its length as handed out.
func (h *heapReader) object(addr uint64, index uint32) ([]byte, error) {
	collection, ok := h.cache[addr]
	if !ok {
		var err error
		collection, err = ReadGlobalHeapCollection(h.r, addr, int(h.sb.OffsetSize))
		if err != nil {
			return nil, fmt.Errorf("failed to read global heap collection at 0x%X: %w", addr, err)
		}

		h.cachedBytes += collection.Size
		if h.cachedBytes > h.limit {
			return nil, fmt.Errorf("%w: global heap collections of this read hold %d bytes, more than the read limit of %d bytes; "+
				"raise it with (*hdf5.File).SetReadLimit", utils.ErrLimit, h.cachedBytes, h.limit)
		}
		h.cache[addr] = collection
	}

	obj, err := collection.GetObject(index)
	if err != nil {
		return nil, fmt.Errorf("failed to get object %d from heap collection: %w", index, err)
	}

	h.handedOut += uint64(len(obj.Data))
	if h.handedOut > h.limit {
		return nil, fmt.Errorf("%w: variable-length data of this read needs %d bytes, more than the read limit of %d bytes; "+
			"raise it with (*hdf5.File).SetReadLimit", utils.ErrLimit, h.handedOut, h.limit)
	}

	return obj.Data, nil
}

// readString dereferences a variable-length string reference.
func (h *heapReader) readString(data []byte) (string, error) {
	ref, err := ParseGlobalHeapReference(data, int(h.sb.OffsetSize))
	if err != nil {
		return "", fmt.Errorf("failed to parse global heap reference: %w", err)
	}

	// Check for null reference (address 0 typically means empty/null string).
	if ref.HeapAddress == 0 {
		return "", nil
	}

	obj, err := h.object(ref.HeapAddress, ref.ObjectIndex)
	if err != nil {
		return "", err
	}

	// Variable-length strings are typically null-terminated.
	end := len(obj)
	for end > 0 && obj[end-1] == 0 {
		end--
	}

	return string(obj[:end]), nil
}
