package writer

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"hash/adler32"
	"io"
)

// HDF5 filter label for DEFLATE / GZIP compression. Extracted as a
// constant so goconst doesn't flag the duplicate string across source +
// helper tests.
const filterDeflateName = "deflate"

// GZIPFilter implements the HDF5 deflate filter (FilterID = 1).
// Despite the historical "GZIP" naming, the stored stream is zlib format
// (RFC 1950) — what the C library's compress2() produces — NOT gzip
// (RFC 1952). Do not switch this back to compress/gzip: the C tools
// cannot read gzip-wrapped chunks (verified by TestCInterop_WriteMatrix).
//
// Compression levels:
//
//	0 = no compression (stored blocks)
//	1 = fastest compression, larger files
//	6 = balanced (default)
//	9 = best compression, slower
type GZIPFilter struct {
	level int // Compression level (0-9)
}

// NewGZIPFilter creates a deflate (zlib) filter with the specified compression level.
//
// Valid levels:
//
//	0 = No compression: the data is stored in zlib framing, as H5Pset_deflate(plist, 0) does
//	1 = Fast compression, lower ratio
//	6 = Default (balanced)
//	9 = Best compression, slower
//
// Invalid levels are automatically adjusted to 6 (default).
func NewGZIPFilter(level int) *GZIPFilter {
	if level < 0 || level > 9 {
		level = 6 // Default compression level
	}
	return &GZIPFilter{level: level}
}

// ID returns the HDF5 filter identifier for GZIP.
func (f *GZIPFilter) ID() FilterID {
	return FilterGZIP
}

// Name returns the HDF5 filter name.
// HDF5 uses "deflate" (the underlying algorithm) rather than "gzip".
func (f *GZIPFilter) Name() string {
	return filterDeflateName
}

// Apply compresses data using the DEFLATE algorithm in zlib format
// (RFC 1950). The HDF5 deflate filter stores zlib streams (what the C
// library's compress2() produces), NOT gzip-wrapped (RFC 1952) streams.
func (f *GZIPFilter) Apply(data []byte) ([]byte, error) {
	if f.level == 0 {
		return zlibStored(data), nil
	}

	var buf bytes.Buffer

	w, err := zlib.NewWriterLevel(&buf, f.level)
	if err != nil {
		return nil, fmt.Errorf("zlib writer creation failed: %w", err)
	}

	// Compress data
	if _, err := w.Write(data); err != nil {
		_ = w.Close() // Ignore close error on write failure
		return nil, fmt.Errorf("zlib compression failed: %w", err)
	}

	// Flush and close to ensure all data is written
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("zlib close failed: %w", err)
	}

	return buf.Bytes(), nil
}

// maxStoredBlock is the largest DEFLATE stored block (RFC 1951, section 3.2.4).
const maxStoredBlock = 65535

// zlibStored returns data as a zlib stream of stored blocks, byte for byte what
// C zlib's compress2() writes at level 0: a final block ends the stream, where
// compress/zlib at NoCompression appends an empty one, five bytes longer.
func zlibStored(data []byte) []byte {
	blocks := max(1, (len(data)+maxStoredBlock-1)/maxStoredBlock)
	out := make([]byte, 0, 2+5*blocks+len(data)+4)
	out = append(out, 0x78, 0x01) // 32 KiB window, FLEVEL 0, as deflate.c writes for level 0

	for i := range blocks {
		block := data[i*maxStoredBlock : min(len(data), (i+1)*maxStoredBlock)]
		var final byte
		if i == blocks-1 {
			final = 1
		}
		n := uint16(len(block)) //nolint:gosec // G115: a stored block is at most 65535 bytes
		out = append(out, final)
		out = binary.LittleEndian.AppendUint16(out, n)
		out = binary.LittleEndian.AppendUint16(out, ^n)
		out = append(out, block...)
	}
	return binary.BigEndian.AppendUint32(out, adler32.Checksum(data))
}

// Remove decompresses zlib-compressed data.
// Returns the original uncompressed data.
//
// This method reverses the Apply operation, restoring the original data.
func (f *GZIPFilter) Remove(data []byte) ([]byte, error) {
	buf := bytes.NewReader(data)

	r, err := zlib.NewReader(buf)
	if err != nil {
		return nil, fmt.Errorf("zlib reader creation failed: %w", err)
	}
	defer func() { _ = r.Close() }() // Ignore error in defer

	// Decompress data
	decompressed, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("zlib decompression failed: %w", err)
	}

	return decompressed, nil
}

// Encode returns the filter parameters for the Pipeline message.
//
// For GZIP, the client data contains a single value: the compression level.
// The filter is optional, as H5Pset_deflate adds it (H5Pocpl.c:1065).
func (f *GZIPFilter) Encode() (flags uint16, cdValues []uint32) {
	return FilterFlagOptional, []uint32{uint32(f.level)} //nolint:gosec // G115: Compression level is 0-9, always fits in uint32
}
