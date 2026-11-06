package core

import (
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/scigolib/hdf5/internal/utils"
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

// Human-readable filter labels. Extracted as constants so goconst doesn't
// flag the string-literal duplication across source + helper tests.
const (
	filterGZIPName = "GZIP"
	filterSZIPName = "SZIP"
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

// Filter pipeline message constants (H5Opline.c, H5Zpublic.h).
const (
	pipelineVersion1 uint8 = 1 // H5O_PLINE_VERSION_1: padded names and client data.
	pipelineVersion2 uint8 = 2 // H5O_PLINE_VERSION_2: no padding, names only for id >= 256.

	// maxPipelineFilters is H5Z_MAX_NFILTERS (H5Zpublic.h:111).
	maxPipelineFilters = 32

	// filterReserved is H5Z_FILTER_RESERVED (H5Zpublic.h:93): filters below it
	// are the library's own, and a version 2 message stores no name for them.
	filterReserved FilterID = 256
)

// ParseFilterPipelineMessage decodes a filter pipeline message (type 0x000B)
// as H5O__pline_decode does (H5Opline.c:109-255):
//
//	version 1: version, filter count, 6 reserved bytes; per filter: id, name
//	           length (a multiple of 8, the padded name), flags, client data
//	           count, the name, the client data and 4 bytes of padding after
//	           an odd count;
//	version 2: version, filter count; per filter: id, a name length only when
//	           id >= 256 (H5Z_FILTER_RESERVED), flags, client data count, the
//	           name unpadded, the client data unpadded.
//
// Every bound C checks is checked; a violation wraps utils.ErrCorrupt.
// The name is read up to its NUL terminator, which must lie in the message
// (H5Opline.c:192-197).
func ParseFilterPipelineMessage(data []byte) (*FilterPipelineMessage, error) {
	c := pipelineCursor{data: data}

	version, err := c.u8()
	if err != nil {
		return nil, err
	}
	if version < pipelineVersion1 || version > pipelineVersion2 {
		return nil, fmt.Errorf("%w: bad version number %d for filter pipeline message (H5Opline.c:133)",
			utils.ErrCorrupt, version)
	}

	numFilters, err := c.u8()
	if err != nil {
		return nil, err
	}
	if numFilters > maxPipelineFilters {
		return nil, fmt.Errorf("%w: filter pipeline message has %d filters, more than %d (H5Opline.c:139-147)",
			utils.ErrCorrupt, numFilters, maxPipelineFilters)
	}

	if version == pipelineVersion1 {
		if err := c.skip(6); err != nil { // Reserved.
			return nil, err
		}
	}

	pipeline := &FilterPipelineMessage{
		Version:    version,
		NumFilters: numFilters,
		Filters:    make([]Filter, 0, numFilters),
	}

	for i := uint8(0); i < numFilters; i++ {
		filter, err := c.filter(version)
		if err != nil {
			return nil, fmt.Errorf("filter %d: %w", i, err)
		}
		pipeline.Filters = append(pipeline.Filters, filter)
	}

	return pipeline, nil
}

// pipelineCursor reads a filter pipeline message with C's bounds checks
// (H5_IS_BUFFER_OVERFLOW before every field).
type pipelineCursor struct {
	data []byte
	off  int
}

// overflow is C's "ran off end of input buffer while decoding".
func (c *pipelineCursor) overflow(n int) error {
	return fmt.Errorf("%w: filter pipeline message ran off the end of its %d bytes reading %d bytes at %d",
		utils.ErrCorrupt, len(c.data), n, c.off)
}

func (c *pipelineCursor) skip(n int) error {
	if n > len(c.data)-c.off {
		return c.overflow(n)
	}
	c.off += n
	return nil
}

func (c *pipelineCursor) u8() (uint8, error) {
	if err := c.skip(1); err != nil {
		return 0, err
	}
	return c.data[c.off-1], nil
}

func (c *pipelineCursor) u16() (uint16, error) {
	if err := c.skip(2); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint16(c.data[c.off-2:]), nil
}

func (c *pipelineCursor) u32() (uint32, error) {
	if err := c.skip(4); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(c.data[c.off-4:]), nil
}

// filter decodes one filter description (H5Opline.c:166-241).
func (c *pipelineCursor) filter(version uint8) (Filter, error) {
	var f Filter

	id, err := c.u16()
	if err != nil {
		return f, err
	}
	f.ID = FilterID(id)

	if version == pipelineVersion1 || f.ID >= filterReserved {
		if f.NameLength, err = c.u16(); err != nil {
			return f, err
		}
		if version == pipelineVersion1 && f.NameLength%8 != 0 {
			return f, fmt.Errorf("%w: filter name length %d is not a multiple of eight (H5Opline.c:175-176)",
				utils.ErrCorrupt, f.NameLength)
		}
	}

	if f.Flags, err = c.u16(); err != nil {
		return f, err
	}
	if f.NumClientData, err = c.u16(); err != nil {
		return f, err
	}

	if f.NameLength > 0 {
		rest := c.data[c.off:]
		end := bytes.IndexByte(rest, 0)
		if end < 0 {
			return f, fmt.Errorf("%w: filter name not null terminated (H5Opline.c:196-197)", utils.ErrCorrupt)
		}
		f.Name = string(rest[:end])
		if err := c.skip(int(f.NameLength)); err != nil {
			return f, err
		}
	}

	if f.NumClientData > 0 {
		// Bounded by the message before allocating: each value takes 4 bytes.
		if int(f.NumClientData) > (len(c.data)-c.off)/4 {
			return f, c.overflow(int(f.NumClientData) * 4)
		}
		f.ClientData = make([]uint32, f.NumClientData)
		for j := range f.ClientData {
			if f.ClientData[j], err = c.u32(); err != nil {
				return f, err
			}
		}
		if version == pipelineVersion1 && f.NumClientData%2 != 0 {
			if err := c.skip(4); err != nil { // Padding (H5Opline.c:235-241).
				return f, err
			}
		}
	}

	return f, nil
}

// ApplyFilters applies filter pipeline to decompress/decode chunk data.
//
// maxOutput is the chunk's byte size from the layout (DatasetDescriptor.ChunkBytes):
// a valid pipeline reproduces exactly that many bytes, so no filter may produce
// more, plus 4 bytes per Fletcher32 checksum still to be stripped. The bound is
// applied while decompressing (io.LimitReader), because a deflate or bzip2
// stream expands about a thousandfold and the C library's H5Z output buffer is
// likewise sized from the chunk (review finding H4). A larger output wraps
// utils.ErrCorrupt.
func (fp *FilterPipelineMessage) ApplyFilters(data []byte, maxOutput uint64) ([]byte, error) {
	return fp.ApplyFiltersMasked(data, 0, maxOutput)
}

// ApplyFiltersMasked is ApplyFilters for a chunk whose filter mask is mask:
// bit i set means filter i was not applied when the chunk was written (an
// optional filter that failed), so it is skipped on read, as H5Z_pipeline
// does (H5Z.c:1400-1403).
func (fp *FilterPipelineMessage) ApplyFiltersMasked(data []byte, mask uint32, maxOutput uint64) ([]byte, error) {
	if fp == nil || len(fp.Filters) == 0 {
		return data, nil
	}

	// Filters are applied in REVERSE order during decompression.
	// (they were applied forward during compression).
	result := data
	var err error

	for i := len(fp.Filters) - 1; i >= 0; i-- {
		filter := fp.Filters[i]
		if i < 32 && mask&(uint32(1)<<uint(i)) != 0 {
			continue // Not applied when this chunk was written.
		}

		// The optional flag (H5Z_FLAG_OPTIONAL) matters only when writing. On
		// read H5Z_pipeline fails for a filter it does not have (H5Z.c:1408-1434)
		// and for a filter that fails (H5Z.c:1475-1490), optional or not:
		// skipping it would hand the still-encoded bytes on as data.
		result, err = applyFilter(filter, result, maxOutput+4*fletcherBefore(fp.Filters, i))
		if err != nil {
			return nil, filterError(filter, err)
		}

		// LZF filter: ensure output matches expected size from cd_values[2].
		// The HDF5 LZF filter stores the expected uncompressed chunk size in cd_values[2].
		// If decompression produces less data than expected, pad with zeros.
		// This can happen with sparse chunks or chunks at dataset boundaries.
		if filter.ID == FilterLZF && len(filter.ClientData) >= 3 && filter.ClientData[2] > 0 {
			// ADR-003: the padding buffer is sized from cd_values[2], i.e. from
			// the file, and a corrupt value would ask for an allocation Go turns
			// into a fatal throw. The chunk policy has been 1 GB since
			// CVE-2025-7067, so the same cap applies here instead of a second,
			// looser one. The check runs on the raw uint32, before any
			// conversion.
			chunkSize := uint64(filter.ClientData[2])
			if err := utils.ValidateBufferSize(chunkSize, utils.MaxChunkSize, "LZF uncompressed chunk"); err != nil {
				return nil, fmt.Errorf("filter %d (%s): %w", filter.ID, filterName(filter.ID), err)
			}

			// cd_values[2] is the uncompressed chunk size, a uint32 from the
			// filter pipeline message. Convert with the platform check: on a
			// 32-bit target a value above math.MaxInt32 would wrap negative and
			// the padding below would be skipped silently.
			expectedSize, convErr := utils.SafeInt(chunkSize, "LZF uncompressed chunk size")
			if convErr != nil {
				return nil, fmt.Errorf("filter %d (%s): %w", filter.ID, filterName(filter.ID), convErr)
			}

			if chunkSize > maxOutput {
				return nil, fmt.Errorf("%w: filter %d (%s): uncompressed size %d exceeds the chunk size %d",
					utils.ErrCorrupt, filter.ID, filterName(filter.ID), chunkSize, maxOutput)
			}

			if len(result) < expectedSize {
				// Pad with zeros to match expected size
				padded := make([]byte, expectedSize)
				copy(padded, result)
				result = padded
			}
		}
	}

	return result, nil
}

// ErrFilterNotSupported marks data stored through a filter this library cannot
// decode (N-bit, scale-offset, SZIP, an unregistered third-party filter). The
// file is valid HDF5; it wraps utils.ErrNotSupported, not utils.ErrCorrupt.
var ErrFilterNotSupported = fmt.Errorf("hdf5: filter not supported: %w", utils.ErrNotSupported)

// filterError classifies a filter failure on read: a filter we cannot decode
// keeps ErrFilterNotSupported; a supported filter that fails on the stored
// bytes is corruption, unless it already reports a limit or corruption.
func filterError(filter Filter, err error) error {
	name := filterName(filter.ID)
	switch {
	case errors.Is(err, ErrFilterNotSupported):
		return fmt.Errorf("required filter %d (%s) is not supported: %w", filter.ID, name, err)
	case errors.Is(err, utils.ErrCorrupt), errors.Is(err, utils.ErrLimit):
		return fmt.Errorf("filter %d (%s) failed: %w", filter.ID, name, err)
	default:
		return fmt.Errorf("%w: filter %d (%s) failed: %w", utils.ErrCorrupt, filter.ID, name, err)
	}
}

// applyFilter applies a single filter.
func applyFilter(filter Filter, data []byte, maxOutput uint64) ([]byte, error) {
	switch filter.ID {
	case FilterDeflate:
		return applyDeflate(data, maxOutput)

	case FilterShuffle:
		return applyShuffle(data, filter.ClientData)

	case FilterFletcher:
		// Fletcher32 is a checksum - just verify and strip it.
		return applyFletcher32(data)

	case FilterBZIP2:
		return applyBZIP2(data, maxOutput)

	case FilterLZF:
		// LZF filter: check if data is actually uncompressed.
		// HDF5 stores data uncompressed if compression doesn't help.
		// cd_values[2] contains the expected uncompressed chunk size.
		if len(filter.ClientData) >= 3 && filter.ClientData[2] > 0 {
			// Same conversion guard as ApplyFilters: the value is a uint32 from
			// the file and must fit in int before the comparison.
			expectedSize, err := utils.SafeInt(uint64(filter.ClientData[2]), "LZF uncompressed chunk size")
			if err != nil {
				return nil, fmt.Errorf("filter %d (%s): %w", filter.ID, filterName(filter.ID), err)
			}

			if len(data) == expectedSize {
				// Data is already uncompressed (compression didn't help)
				return data, nil
			}
		}
		return applyLZF(data, maxOutput)

	case FilterSZIP:
		return applySZIP(data)

	default:
		// N-bit (H5Znbit.c), scale-offset (H5Zscaleoffset.c) and any
		// third-party filter: no decoder here.
		return nil, fmt.Errorf("unsupported filter ID: %d: %w", filter.ID, ErrFilterNotSupported)
	}
}

// applyDeflate decompresses GZIP/deflate compressed data.
// HDF5 uses raw deflate (zlib), not gzip format.
//
// Compatibility fallback: releases of this library up to v0.14 wrote
// gzip-wrapped (RFC 1952) streams instead of zlib (RFC 1950). Sniff the
// gzip magic so those files stay readable.
func applyDeflate(data []byte, maxOutput uint64) ([]byte, error) {
	var reader io.ReadCloser
	var err error
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		reader, err = gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("gzip reader creation failed: %w", err)
		}
	} else {
		reader, err = zlib.NewReader(io.NopCloser(io.NewSectionReader(
			&bytesReaderAt{data}, 0, int64(len(data)))))
		if err != nil {
			return nil, fmt.Errorf("zlib reader creation failed: %w", err)
		}
	}
	defer func() { _ = reader.Close() }()

	// Read all decompressed data.
	decompressed, err := readBounded(reader, maxOutput)
	if err != nil {
		return nil, fmt.Errorf("deflate decompression failed: %w", err)
	}

	return decompressed, nil
}

// applyShuffle reverses shuffle filter.
// Shuffle reorders bytes to improve compression.
//
// As H5Z__filter_shuffle (H5Zshuffle.c:108-290): exactly one parameter, a
// non-zero element size (:125-126); fewer than two whole elements or a 1-byte
// type leave the buffer as it is (:133-136); bytes past the last whole element
// stay in place at the end (:279-284).
func applyShuffle(data []byte, clientData []uint32) ([]byte, error) {
	if len(clientData) != 1 || clientData[0] == 0 {
		return nil, fmt.Errorf("%w: invalid shuffle parameters %v (H5Zshuffle.c:125-126)", utils.ErrCorrupt, clientData)
	}

	if uint64(clientData[0]) > uint64(len(data)) {
		return data, nil // Fewer than one whole element.
	}
	elementSize := int(clientData[0])
	numElements := len(data) / elementSize
	if elementSize <= 1 || numElements <= 1 {
		return data, nil
	}

	result := make([]byte, len(data))
	whole := numElements * elementSize
	copy(result[whole:], data[whole:]) // Leftover bytes.

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

// applyFletcher32 verifies and strips the Fletcher32 checksum of a chunk, as
// H5Z__filter_fletcher32 does on read (H5Zfletcher32.c:64-104): the last 4
// bytes are the checksum, little-endian (UINT32DECODE), of the bytes before
// them. C accepts the checksum and its pre-1.6.3 variant, whose bytes are
// swapped within each 16-bit half (the "reversed" checksum, H5Zfletcher32.c:
// 79-95), so files written by HDF5 1.6.2 and earlier still read. A mismatch,
// or a chunk too short to hold a checksum, wraps utils.ErrCorrupt ("data
// error detected by Fletcher32 checksum").
//
// C skips the check when error detection is disabled for the transfer
// (H5Pset_edc_check, H5Z_FLAG_SKIP_EDC); this library has no such switch and
// always verifies.
func applyFletcher32(data []byte) ([]byte, error) {
	if len(data) < fletcherLen {
		return nil, fmt.Errorf("%w: %d bytes cannot hold a Fletcher32 checksum", utils.ErrCorrupt, len(data))
	}

	n := len(data) - fletcherLen
	stored := binary.LittleEndian.Uint32(data[n:])
	sum := fletcher32(data[:n])
	reversed := (sum&0x00FF00FF)<<8 | (sum&0xFF00FF00)>>8

	if stored != sum && stored != reversed {
		return nil, fmt.Errorf("%w: data error detected by Fletcher32 checksum: stored 0x%08X, computed 0x%08X",
			utils.ErrCorrupt, stored, sum)
	}

	return data[:n], nil
}

// fletcherLen is FLETCHER_LEN, the size of the stored checksum
// (H5Zfletcher32.c).
const fletcherLen = 4

// fletcher32 is H5_checksum_fletcher32 (H5checksum.c:94-136): 16-bit words
// read big-endian, an odd last byte as the high half of a word, both sums
// folded to 16 bits after every 360 words (the most that cannot overflow) and
// once more at the end. The folding keeps 0xFFFF where a modulo would give 0;
// the result must match C bit for bit.
func fletcher32(data []byte) uint32 {
	var sum1, sum2 uint32

	p := data
	for words := len(data) / 2; words > 0; {
		block := min(words, 360)
		words -= block
		for ; block > 0; block-- {
			sum1 += uint32(p[0])<<8 | uint32(p[1])
			p = p[2:]
			sum2 += sum1
		}
		sum1 = sum1&0xFFFF + sum1>>16
		sum2 = sum2&0xFFFF + sum2>>16
	}

	if len(data)%2 == 1 {
		sum1 += uint32(p[0]) << 8
		sum2 += sum1
		sum1 = sum1&0xFFFF + sum1>>16
		sum2 = sum2&0xFFFF + sum2>>16
	}

	sum1 = sum1&0xFFFF + sum1>>16
	sum2 = sum2&0xFFFF + sum2>>16

	return sum2<<16 | sum1
}

// applyBZIP2 decompresses BZIP2-compressed data.
// BZIP2 is a high-compression algorithm providing better compression than GZIP.
// Uses stdlib compress/bzip2 for decompression.
func applyBZIP2(data []byte, maxOutput uint64) ([]byte, error) {
	reader := bzip2.NewReader(io.NopCloser(io.NewSectionReader(
		&bytesReaderAt{data}, 0, int64(len(data)))))

	// Read all decompressed data.
	decompressed, err := readBounded(reader, maxOutput)
	if err != nil {
		return nil, fmt.Errorf("bzip2 decompression failed: %w", err)
	}

	return decompressed, nil
}

// applyLZF decompresses LZF-compressed data.
// LZF is a very fast compression algorithm used by PyTables and h5py.
func applyLZF(data []byte, maxOutput uint64) ([]byte, error) {
	if len(data) == 0 {
		return data, nil
	}

	decompressed, err := lzfDecompress(data, maxOutput)
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
	return nil, fmt.Errorf("SZIP decompression requires libaec library (not available in pure Go); "+
		"SZIP uses extended Golomb-Rice coding (CCSDS 121.0-B-3 standard); "+
		"to read SZIP-compressed datasets, use the HDF5 C library or h5py; "+
		"alternatively, re-save the file with GZIP compression (filter ID 1): %w", ErrFilterNotSupported)
}

// lzfDecompress decompresses LZF-compressed data.
// LZF format consists of segments:
//   - Literal run (000LLLLL): L+1 bytes of uncompressed data
//   - Short backref (RRROXXXX XXXXXXXX): 3-8 bytes from offset 1-8192
//   - Long backref (111OXXXX XXXXXXXX RRRRRRRR): 9-264 bytes from offset 1-8192
func lzfDecompress(input []byte, maxOutput uint64) ([]byte, error) {
	inLen := len(input)
	if inLen == 0 {
		return input, nil
	}

	// Pre-allocate output buffer (LZF typically achieves 40-50% compression),
	// never beyond what the chunk may hold.
	output := make([]byte, 0, min(uint64(inLen)*2, maxOutput))
	inPos := 0

	for inPos < inLen {
		// Read control byte.
		ctrl := input[inPos]
		inPos++

		// Check segment type based on top 3 bits.
		if (ctrl & 0xE0) == 0 {
			// Literal run: 000LLLLL
			runLen := int(ctrl) + 1

			if inPos+runLen > len(input) {
				return nil, errors.New("lzf: truncated literal run")
			}

			if uint64(len(output)+runLen) > maxOutput {
				return nil, lzfOverflow(maxOutput)
			}
			output = append(output, input[inPos:inPos+runLen]...)
			inPos += runLen
		} else {
			// Backreference (short or long).
			if inPos >= len(input) {
				return nil, errors.New("lzf: truncated backreference")
			}

			// Read offset (13 bits across 2 bytes).
			offsetHigh := int(ctrl & 0x1F)
			offsetLow := int(input[inPos]) //nolint:gosec // G602: inPos < len(input), checked above.
			inPos++

			offset := (offsetHigh << 8) | offsetLow
			offset++ // Offset is 1-based in encoding

			// Determine run length.
			var runLen int
			if (ctrl & 0xE0) == 0xE0 {
				// Long backreference: 111OXXXX XXXXXXXX RRRRRRRR
				if inPos >= len(input) {
					return nil, errors.New("lzf: truncated long backreference")
				}
				runLen = int(input[inPos]) + 9 //nolint:gosec // G602: inPos < len(input), checked above.
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

			if uint64(len(output)+runLen) > maxOutput {
				return nil, lzfOverflow(maxOutput)
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

// lzfOverflow reports an LZF stream that expands beyond the chunk.
func lzfOverflow(maxOutput uint64) error {
	return fmt.Errorf("%w: lzf: decompressed data exceeds the chunk size %d", utils.ErrCorrupt, maxOutput)
}

// readBounded reads a decompressor's output, refusing more than maxOutput
// bytes. It reads one byte past the bound so an oversized stream is detected
// without being buffered.
func readBounded(r io.Reader, maxOutput uint64) ([]byte, error) {
	limit := int64(math.MaxInt64)
	if maxOutput < math.MaxInt64 {
		limit = int64(maxOutput) + 1
	}

	out, err := io.ReadAll(io.LimitReader(r, limit))
	if err != nil {
		return nil, err
	}

	if uint64(len(out)) > maxOutput {
		return nil, fmt.Errorf("%w: decompressed data exceeds the chunk size %d", utils.ErrCorrupt, maxOutput)
	}

	return out, nil
}

// fletcherBefore counts the Fletcher32 filters that precede filter i in the
// pipeline. During decoding they run after it, so its output still carries
// their 4-byte checksums.
func fletcherBefore(filters []Filter, i int) uint64 {
	var n uint64
	for _, f := range filters[:i] {
		if f.ID == FilterFletcher {
			n++
		}
	}

	return n
}

// filterName returns human-readable filter name.
func filterName(id FilterID) string {
	switch id {
	case FilterDeflate:
		return filterGZIPName
	case FilterShuffle:
		return "Shuffle"
	case FilterFletcher:
		return "Fletcher32"
	case FilterBZIP2:
		return "BZIP2"
	case FilterLZF:
		return "LZF"
	case FilterSZIP:
		return filterSZIPName
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
