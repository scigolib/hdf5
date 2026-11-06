// Package writer provides HDF5 file writing capabilities.
package writer

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/scigolib/hdf5/internal/core"
)

// FilterID represents HDF5 standard filter identifiers.
type FilterID uint16

// HDF5 standard filter constants.
const (
	FilterNone        FilterID = 0     // No filter
	FilterGZIP        FilterID = 1     // GZIP compression (deflate)
	FilterShuffle     FilterID = 2     // Byte shuffle
	FilterFletcher32  FilterID = 3     // Fletcher32 checksum
	FilterSZIP        FilterID = 4     // SZIP (not implemented)
	FilterNBIT        FilterID = 5     // NBIT (not implemented)
	FilterScaleOffset FilterID = 6     // Scale+offset (not implemented)
	FilterBZIP2       FilterID = 307   // BZIP2 compression
	FilterLZF         FilterID = 32000 // LZF compression (PyTables/h5py)
)

// filterReserved is the first filter ID outside the library's own range
// (H5Z_FILTER_RESERVED, H5Zpublic.h:93); version 2 of the pipeline message
// stores no name for filters below it (H5Opline.c:300).
const filterReserved FilterID = 256

// Filter flags of the pipeline message (H5Zpublic.h:121-125). An optional
// filter may fail on a chunk, which is then stored unfiltered and marked in
// the chunk's filter mask; a mandatory one fails the write.
const (
	FilterFlagMandatory uint16 = 0x0000 // H5Z_FLAG_MANDATORY
	FilterFlagOptional  uint16 = 0x0001 // H5Z_FLAG_OPTIONAL
)

// Filter interface for data transformation.
// Filters are applied in sequence during write (e.g., Shuffle → GZIP → Fletcher32)
// and reversed during read (Fletcher32 → GZIP → Shuffle).
type Filter interface {
	// ID returns the HDF5 filter identifier.
	ID() FilterID

	// Name returns human-readable filter name.
	Name() string

	// Apply applies filter to data (compression/checksum on write path).
	// Returns transformed data.
	Apply(data []byte) ([]byte, error)

	// Remove reverses filter (decompression/verification on read path).
	// Returns original data.
	Remove(data []byte) ([]byte, error)

	// Encode encodes filter parameters for Pipeline message.
	// Returns: flags, cd_values (client data array).
	Encode() (flags uint16, cdValues []uint32)
}

// FilterPipeline manages a chain of filters applied to chunk data.
// Filters are applied in sequence on write and reversed on read.
//
// Example pipeline for numeric data compression:
//  1. Shuffle (reorder bytes for better compression)
//  2. GZIP (compress data)
//  3. Fletcher32 (add checksum)
//
// On write: data → Shuffle → GZIP → Fletcher32 → stored.
// On read:  stored → Fletcher32 → GZIP → Shuffle → data.
type FilterPipeline struct {
	filters []Filter
}

// NewFilterPipeline creates an empty filter pipeline.
func NewFilterPipeline() *FilterPipeline {
	return &FilterPipeline{
		filters: make([]Filter, 0),
	}
}

// AddFilter adds a filter to the end of the pipeline.
// Filters are applied in the order they are added during write operations.
func (fp *FilterPipeline) AddFilter(f Filter) {
	fp.filters = append(fp.filters, f)
}

// AddFilterAtStart inserts a filter at the beginning of the pipeline.
// This is useful for filters that should be applied first (e.g., Shuffle before GZIP).
func (fp *FilterPipeline) AddFilterAtStart(f Filter) {
	fp.filters = append([]Filter{f}, fp.filters...)
}

// Apply applies all filters in sequence (write path).
// Example: Shuffle → GZIP → Fletcher32
//
// If any filter fails, the operation stops and returns an error.
func (fp *FilterPipeline) Apply(data []byte) ([]byte, error) {
	result := data
	for _, filter := range fp.filters {
		var err error
		result, err = filter.Apply(result)
		if err != nil {
			return nil, fmt.Errorf("filter %s failed: %w", filter.Name(), err)
		}
	}
	return result, nil
}

// Remove reverses all filters in reverse order (read path).
// Example: Fletcher32 → GZIP → Shuffle
//
// Filters must be removed in reverse order to correctly restore the original data.
func (fp *FilterPipeline) Remove(data []byte) ([]byte, error) {
	result := data
	// Apply in REVERSE order
	for i := len(fp.filters) - 1; i >= 0; i-- {
		filter := fp.filters[i]
		var err error
		result, err = filter.Remove(result)
		if err != nil {
			return nil, fmt.Errorf("filter %s remove failed: %w", filter.Name(), err)
		}
	}
	return result, nil
}

// IsEmpty returns true if the pipeline has no filters.
func (fp *FilterPipeline) IsEmpty() bool {
	return len(fp.filters) == 0
}

// Count returns the number of filters in the pipeline.
func (fp *FilterPipeline) Count() int {
	return len(fp.filters)
}

// EncodePipelineMessage encodes the filter pipeline as an HDF5 filter
// pipeline message (0x000B) of the given version, byte for byte as
// H5O__pline_encode writes it (H5Opline.c:267-350):
//
//   - header: version, number of filters and, in version 1 only, six reserved
//     zero bytes;
//   - per filter: ID, name length, flags, number of client data values, the
//     NUL-terminated name and the client data values. Version 1 pads the name
//     to a multiple of 8 bytes (and stores the padded length) and adds a zero
//     value after an odd number of values; version 2 does neither and stores
//     no name length or name for a filter below 256 (H5Opline.c:300).
//
// It returns an error for an empty pipeline, more filters than the format
// allows or an unknown version.
func (fp *FilterPipeline) EncodePipelineMessage(version uint8) ([]byte, error) {
	if fp.IsEmpty() {
		return nil, errors.New("empty filter pipeline")
	}
	if version != core.PipelineVersion1 && version != core.PipelineVersion2 {
		return nil, fmt.Errorf("filter pipeline message version %d is not 1 or 2", version)
	}
	if len(fp.filters) > maxFilters {
		return nil, fmt.Errorf("filter pipeline has %d filters, the format allows %d", len(fp.filters), maxFilters)
	}

	buf := make([]byte, 0, 8+len(fp.filters)*32)
	buf = append(buf, version, byte(len(fp.filters))) //nolint:gosec // G115: at most maxFilters, checked above
	if version == core.PipelineVersion1 {
		buf = append(buf, 0, 0, 0, 0, 0, 0) // reserved
	}
	for _, filter := range fp.filters {
		var err error
		if buf, err = appendFilter(buf, filter, version); err != nil {
			return nil, err
		}
	}
	return buf, nil
}

// maxFilters is H5Z_MAX_NFILTERS (H5Zpublic.h:111): the C library holds no
// more filters in one pipeline.
const maxFilters = 32

// appendFilter appends the encoding of filter f in a pipeline message of the
// given version to buf (H5Opline.c:292-348).
func appendFilter(buf []byte, f Filter, version uint8) ([]byte, error) {
	flags, cdValues := f.Encode()
	if len(cdValues) > 0xFFFF {
		return nil, fmt.Errorf("filter %s has %d client data values, the format allows 65535", f.Name(), len(cdValues))
	}

	// The name is written NUL-terminated; version 1 pads it to 8 bytes and
	// stores the padded length (H5O_ALIGN_OLD), version 2 writes none for the
	// library's own filters.
	var name string
	if version == core.PipelineVersion1 || f.ID() >= filterReserved {
		name = f.Name()
	}
	nameLen := 0
	if name != "" {
		nameLen = len(name) + 1
	}
	storedNameLen := nameLen
	if version == core.PipelineVersion1 {
		storedNameLen = (nameLen + 7) &^ 7
	}
	if storedNameLen > 0xFFFF {
		return nil, fmt.Errorf("filter name of %d bytes is too long for the pipeline message", len(name))
	}

	buf = binary.LittleEndian.AppendUint16(buf, uint16(f.ID()))
	if version == core.PipelineVersion1 || f.ID() >= filterReserved {
		buf = binary.LittleEndian.AppendUint16(buf, uint16(storedNameLen))
	}
	buf = binary.LittleEndian.AppendUint16(buf, flags)
	buf = binary.LittleEndian.AppendUint16(buf, uint16(len(cdValues))) //nolint:gosec // G115: <= 0xFFFF, checked above
	if nameLen > 0 {
		buf = append(buf, name...)
		buf = append(buf, make([]byte, storedNameLen-len(name))...) // NUL and, in version 1, the padding
	}
	for _, v := range cdValues {
		buf = binary.LittleEndian.AppendUint32(buf, v)
	}
	if version == core.PipelineVersion1 && len(cdValues)%2 != 0 {
		buf = binary.LittleEndian.AppendUint32(buf, 0)
	}
	return buf, nil
}
