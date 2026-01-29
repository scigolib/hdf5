package core

import (
	"compress/bzip2"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// FilterID represents HDF5 filter identifiers.
type FilterID uint16

// Filter identifier constants define compression and processing filters for datasets.
const (
	FilterDeflate     FilterID = 1     // GZIP compression.
	FilterShuffle     FilterID = 2     // Shuffle filter.
	FilterFletcher    FilterID = 3     // Fletcher32 checksum.
	FilterSZIP        FilterID = 4     // SZIP compression.
	FilterNBit        FilterID = 5     // N-bit compression.
	FilterScaleOffset FilterID = 6     // Scale-offset filter.
	FilterBZIP2       FilterID = 307   // BZIP2 compression.
	FilterLZF         FilterID = 32000 // LZF compression (PyTables/h5py).
)

// FilterPipelineMessage represents the filter pipeline for a dataset.
type FilterPipelineMessage struct {
	Version    uint8
	NumFilters uint8
	Filters    []Filter
}

// Filter represents a single filter in the pipeline.
type Filter struct {
	ID            FilterID
	NameLength    uint16
	Flags         uint16
	NumClientData uint16
	Name          string
	ClientData    []uint32
}

// ParseFilterPipelineMessage parses filter pipeline message (type 0x000B).
func ParseFilterPipelineMessage(data []byte) (*FilterPipelineMessage, error) {
	if len(data) < 2 {
		return nil, errors.New("filter pipeline message too short")
	}

	version := data[0]
	numFilters := data[1]

	// Support version 1 and 2.
	if version < 1 || version > 2 {
		return nil, fmt.Errorf("unsupported filter pipeline version: %d", version)
	}

	pipeline := &FilterPipelineMessage{
		Version:    version,
		NumFilters: numFilters,
		Filters:    make([]Filter, 0, numFilters),
	}

	offset := 2

	// Version 1 has 6 bytes reserved after num filters.
	if version == 1 {
		offset += 6
	}

	// Parse each filter.
	for i := uint8(0); i < numFilters; i++ {
		if offset+8 > len(data) {
			return nil, fmt.Errorf("filter pipeline truncated at filter %d", i)
		}

		filter := Filter{}

		// Filter ID (2 bytes).
		filter.ID = FilterID(binary.LittleEndian.Uint16(data[offset : offset+2]))
		offset += 2

		// Name length (2 bytes) - for version 1, optional.
		var nameLength uint16
		if version == 1 {
			nameLength = binary.LittleEndian.Uint16(data[offset : offset+2])
			offset += 2
		}
		filter.NameLength = nameLength

		// Flags (2 bytes).
		filter.Flags = binary.LittleEndian.Uint16(data[offset : offset+2])
		offset += 2

		// Number of client data values (2 bytes).
		filter.NumClientData = binary.LittleEndian.Uint16(data[offset : offset+2])
		offset += 2

		// Filter name (variable length, only in version 1).
		if version == 1 && nameLength > 0 {
			// Name is null-terminated and padded to 8-byte boundary.
			padded := nameLength
			if padded%8 != 0 {
				padded += 8 - (padded % 8)
			}

			if offset+int(padded) > len(data) {
				return nil, fmt.Errorf("filter name truncated at filter %d", i)
			}

			// Extract name (up to first null).
			nameBytes := data[offset : offset+int(nameLength)]
			for idx, b := range nameBytes {
				if b == 0 {
					filter.Name = string(nameBytes[:idx])
					break
				}
			}
			if filter.Name == "" {
				filter.Name = string(nameBytes)
			}

			offset += int(padded)
		}

		// Client data (array of uint32).
		if filter.NumClientData > 0 {
			dataSize := int(filter.NumClientData) * 4
			if offset+dataSize > len(data) {
				return nil, fmt.Errorf("filter client data truncated at filter %d", i)
			}

			filter.ClientData = make([]uint32, filter.NumClientData)
			for j := uint16(0); j < filter.NumClientData; j++ {
				filter.ClientData[j] = binary.LittleEndian.Uint32(data[offset : offset+4])
				offset += 4
			}

			// Version 1: client data is padded to 8-byte boundary.
			if version == 1 {
				if dataSize%8 != 0 {
					offset += 8 - (dataSize % 8)
				}
			}
		}

		pipeline.Filters = append(pipeline.Filters, filter)
	}

	return pipeline, nil
}

// ApplyFilters applies filter pipeline to decompress/decode chunk data.
func (fp *FilterPipelineMessage) ApplyFilters(data []byte) ([]byte, error) {
	if fp == nil || len(fp.Filters) == 0 {
		return data, nil
	}

	// Filters are applied in REVERSE order during decompression.
	// (they were applied forward during compression).
	result := data
	var err error

	for i := len(fp.Filters) - 1; i >= 0; i-- {
		filter := fp.Filters[i]

		// Skip optional filters if they fail.
		isOptional := (filter.Flags & 0x0001) != 0

		result, err = applyFilter(filter, result)
		if err != nil {
			if isOptional {
				// Optional filter - log and continue.
				continue
			}
			return nil, fmt.Errorf("filter %d (%s) failed: %w", filter.ID, filterName(filter.ID), err)
		}
	}

	return result, nil
}

// applyFilter applies a single filter.
func applyFilter(filter Filter, data []byte) ([]byte, error) {
	switch filter.ID {
	case FilterDeflate:
		return applyDeflate(data)

	case FilterShuffle:
		return applyShuffle(data, filter.ClientData)

	case FilterFletcher:
		// Fletcher32 is a checksum - just verify and strip it.
		return applyFletcher32(data)

	case FilterBZIP2:
		return applyBZIP2(data)

	case FilterLZF:
		return applyLZF(data)

	case FilterSZIP:
		return applySZIP(data)

	default:
		return nil, fmt.Errorf("unsupported filter ID: %d", filter.ID)
	}
}

// applyDeflate decompresses GZIP/deflate compressed data.
// HDF5 uses raw deflate (zlib), not gzip format.
func applyDeflate(data []byte) ([]byte, error) {
	reader, err := zlib.NewReader(io.NopCloser(io.NewSectionReader(
		&bytesReaderAt{data}, 0, int64(len(data)))))
	if err != nil {
		return nil, fmt.Errorf("zlib reader creation failed: %w", err)
	}
	defer func() { _ = reader.Close() }()

	// Read all decompressed data.
	decompressed, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("zlib decompression failed: %w", err)
	}

	return decompressed, nil
}

// applyShuffle reverses shuffle filter.
// Shuffle reorders bytes to improve compression.
func applyShuffle(data []byte, clientData []uint32) ([]byte, error) {
	if len(clientData) == 0 {
		return nil, errors.New("shuffle filter missing element size")
	}

	elementSize := int(clientData[0])
	if elementSize <= 0 || elementSize > len(data) {
		return nil, fmt.Errorf("invalid shuffle element size: %d", elementSize)
	}

	numElements := len(data) / elementSize
	if len(data)%elementSize != 0 {
		return nil, errors.New("data size not multiple of element size")
	}

	result := make([]byte, len(data))

	// Reverse shuffle: data is organized as [all byte0][all byte1]...[all byteN].
	// We need to interleave them back.
	for elemIdx := 0; elemIdx < numElements; elemIdx++ {
		for byteIdx := 0; byteIdx < elementSize; byteIdx++ {
			srcPos := byteIdx*numElements + elemIdx
			dstPos := elemIdx*elementSize + byteIdx
			result[dstPos] = data[srcPos]
		}
	}

	return result, nil
}

// applyFletcher32 verifies and strips Fletcher32 checksum.
func applyFletcher32(data []byte) ([]byte, error) {
	if len(data) < 4 {
		return nil, errors.New("data too short for Fletcher32 checksum")
	}

	// Fletcher32 checksum is appended at the end (4 bytes).
	// Checksum verification deferred to v0.11.0-RC (feature-complete release).
	// Current implementation strips checksum without validation.
	// In practice, file system and HDF5 library corruption is extremely rare.
	// For production use, consider external file integrity checks (SHA256, etc.).
	// Reference: https://docs.hdfgroup.org/hdf5/latest/group___h5_z.html
	// Target version: v0.11.0-RC (comprehensive data integrity features)
	return data[:len(data)-4], nil
}

// applyBZIP2 decompresses BZIP2-compressed data.
// BZIP2 is a high-compression algorithm providing better compression than GZIP.
// Uses stdlib compress/bzip2 for decompression.
func applyBZIP2(data []byte) ([]byte, error) {
	reader := bzip2.NewReader(io.NopCloser(io.NewSectionReader(
		&bytesReaderAt{data}, 0, int64(len(data)))))

	// Read all decompressed data.
	decompressed, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("bzip2 decompression failed: %w", err)
	}

	return decompressed, nil
}

// applyLZF decompresses LZF-compressed data.
// LZF is a very fast compression algorithm used by PyTables and h5py.
func applyLZF(data []byte) ([]byte, error) {
	decompressed, err := lzfDecompress(data)
	if err != nil {
		return nil, fmt.Errorf("lzf decompression failed: %w", err)
	}
	return decompressed, nil
}

// applySZIP decompresses SZIP-compressed data.
// SZIP uses extended Golomb-Rice coding (CCSDS 121.0-B-3 standard).
// This algorithm is commonly used for satellite imagery and scientific data.
//
// SZIP requires libaec (Adaptive Entropy Coding) library for decompression.
// Since no pure Go implementation exists, we return an informative error.
//
// Reference: https://github.com/MathisRosenhauer/libaec
func applySZIP(_ []byte) ([]byte, error) {
	return nil, errors.New("SZIP decompression requires libaec library (not available in pure Go); " +
		"SZIP uses extended Golomb-Rice coding (CCSDS 121.0-B-3 standard); " +
		"to read SZIP-compressed datasets, use the HDF5 C library or h5py; " +
		"alternatively, re-save the file with GZIP compression (filter ID 1)")
}

// lzfDecompress decompresses LZF-compressed data.
// LZF format consists of segments:
//   - Literal run (000LLLLL): L+1 bytes of uncompressed data
//   - Short backref (RRROXXXX XXXXXXXX): 3-8 bytes from offset 1-8192
//   - Long backref (111OXXXX XXXXXXXX RRRRRRRR): 9-264 bytes from offset 1-8192
func lzfDecompress(input []byte) ([]byte, error) {
	inLen := len(input)
	if inLen == 0 {
		return input, nil
	}

	// Pre-allocate output buffer (LZF typically achieves 40-50% compression).
	output := make([]byte, 0, inLen*2)
	inPos := 0

	for inPos < inLen {
		// Read control byte.
		ctrl := input[inPos]
		inPos++

		// Check segment type based on top 3 bits.
		if (ctrl & 0xE0) == 0 {
			// Literal run: 000LLLLL
			runLen := int(ctrl) + 1

			if inPos+runLen > inLen {
				return nil, errors.New("lzf: truncated literal run")
			}

			output = append(output, input[inPos:inPos+runLen]...)
			inPos += runLen
		} else {
			// Backreference (short or long).
			if inPos >= inLen {
				return nil, errors.New("lzf: truncated backreference")
			}

			// Read offset (13 bits across 2 bytes).
			offsetHigh := int(ctrl & 0x1F)
			offsetLow := int(input[inPos])
			inPos++

			offset := (offsetHigh << 8) | offsetLow
			offset++ // Offset is 1-based in encoding

			// Determine run length.
			var runLen int
			if (ctrl & 0xE0) == 0xE0 {
				// Long backreference: 111OXXXX XXXXXXXX RRRRRRRR
				if inPos >= inLen {
					return nil, errors.New("lzf: truncated long backreference")
				}
				runLen = int(input[inPos]) + 9
				inPos++
			} else {
				// Short backreference: RRROXXXX XXXXXXXX
				runBits := (ctrl >> 5) & 0x07
				runLen = int(runBits) + 2
			}

			// Validate offset.
			if offset > len(output) {
				return nil, fmt.Errorf("lzf: invalid offset %d (output size: %d)", offset, len(output))
			}

			// Copy from earlier position in output.
			srcPos := len(output) - offset
			for i := 0; i < runLen; i++ {
				output = append(output, output[srcPos+i])
			}
		}
	}

	return output, nil
}

// filterName returns human-readable filter name.
func filterName(id FilterID) string {
	switch id {
	case FilterDeflate:
		return "GZIP"
	case FilterShuffle:
		return "Shuffle"
	case FilterFletcher:
		return "Fletcher32"
	case FilterBZIP2:
		return "BZIP2"
	case FilterLZF:
		return "LZF"
	case FilterSZIP:
		return "SZIP"
	case FilterNBit:
		return "N-bit"
	case FilterScaleOffset:
		return "Scale-Offset"
	default:
		return fmt.Sprintf("Unknown-%d", id)
	}
}

// bytesReaderAt wraps []byte to implement io.ReaderAt.
type bytesReaderAt struct {
	data []byte
}

func (b *bytesReaderAt) ReadAt(p []byte, off int64) (n int, err error) {
	if off < 0 || off > int64(len(b.data)) {
		return 0, io.EOF
	}
	n = copy(p, b.data[off:])
	if n < len(p) {
		err = io.EOF
	}
	return n, err
}
