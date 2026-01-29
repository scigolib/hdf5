// Package writer provides HDF5 file writing capabilities.
package writer

import (
	"encoding/binary"
	"errors"
	"fmt"
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

// EncodePipelineMessage encodes the filter pipeline as an HDF5 Pipeline message (0x000B).
// This message is stored in the dataset's object header to describe which filters
// are applied to the data.
//
// Returns the encoded message bytes ready to be written to the object header.
// Returns an error if the pipeline is empty.
func (fp *FilterPipeline) EncodePipelineMessage() ([]byte, error) {
	if fp.IsEmpty() {
		return nil, errors.New("empty filter pipeline")
	}

	// Pipeline message format (version 2):
	// Bytes 0:    Version (1 byte) = 2
	// Bytes 1:    Number of filters (1 byte)
	// Bytes 2-7:  Reserved (6 bytes, must be 0)
	//
	// For each filter:
	//   Filter ID (2 bytes)
	//   Name length (2 bytes) - may be 0
	//   Flags (2 bytes)
	//   Number of CD values (2 bytes)
	//   Name (variable, padded to 8-byte boundary) - only if name length > 0
	//   CD values (4 bytes each)

	buf := make([]byte, 0, 8+len(fp.filters)*32) // Pre-allocate for header + filters
	header := make([]byte, 8)
	header[0] = 2 // Version 2
	header[1] = byte(len(fp.filters))
	// Reserved bytes 2-7 are already zero
	buf = append(buf, header...)

	for _, filter := range fp.filters {
		filterBuf := encodeFilter(filter)
		buf = append(buf, filterBuf...)
	}

	return buf, nil
}

// encodeFilter encodes a single filter for the pipeline message.
func encodeFilter(f Filter) []byte {
	flags, cdValues := f.Encode()
	name := f.Name()
	nameLen := uint16(len(name)) //nolint:gosec // G115: Filter names are short (<256), always fit in uint16

	// Calculate padded name length (align to 8-byte boundary)
	var paddedNameLen uint16
	if nameLen > 0 {
		paddedNameLen = ((nameLen + 7) / 8) * 8
	}

	// Calculate buffer size
	bufSize := 8 + int(paddedNameLen) + len(cdValues)*4
	buf := make([]byte, bufSize)

	// Filter header (8 bytes)
	binary.LittleEndian.PutUint16(buf[0:2], uint16(f.ID()))
	binary.LittleEndian.PutUint16(buf[2:4], nameLen)
	binary.LittleEndian.PutUint16(buf[4:6], flags)
	binary.LittleEndian.PutUint16(buf[6:8], uint16(len(cdValues))) //nolint:gosec // G115: HDF5 limits CD values array to uint16

	offset := 8

	// Name (padded to 8-byte boundary)
	if nameLen > 0 {
		copy(buf[offset:], name)
		offset += int(paddedNameLen)
	}

	// CD values (4 bytes each)
	for _, val := range cdValues {
		binary.LittleEndian.PutUint32(buf[offset:], val)
		offset += 4
	}

	return buf
}
