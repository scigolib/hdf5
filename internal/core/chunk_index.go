package core

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"math/bits"

	"github.com/scigolib/hdf5/internal/utils"
)

// filtered reports whether the dataset has a filter pipeline in use, the
// condition C tests as pline->nused > 0.
func (d *DatasetDescriptor) filtered() bool {
	return d.filterPipeline != nil && len(d.filterPipeline.Filters) > 0
}

// maxChunkGrid returns the number of chunks along each dimension at the
// dataspace's maximum size, as H5D__chunk_set_info_real computes max_chunks
// (H5Dchunk.c:740-762), and the index of the unlimited dimension, -1 when
// there is none. An unlimited dimension has math.MaxUint64 chunks. More than
// one unlimited dimension returns unlimited = -2.
//
// A fixed maximum below the current size is utils.ErrCorrupt: C refuses such a
// dataspace while decoding it (H5O__sdspace_decode, H5Osdspace.c:198-201),
// and the array indexes rely on every current chunk lying inside
// the maximum grid.
func (d *DatasetDescriptor) maxChunkGrid() (maxChunks []uint64, unlimited int, err error) {
	dims := d.dataspace.Dimensions
	maxDims := d.dataspace.MaxDims
	unlimited = -1

	maxChunks = make([]uint64, len(dims))
	for i, dim := range dims {
		m := dim
		if i < len(maxDims) {
			m = maxDims[i]
		}

		if m == undefinedAddress { // H5S_UNLIMITED shares the all-ones encoding.
			maxChunks[i] = math.MaxUint64
			if unlimited == -1 {
				unlimited = i
			} else {
				unlimited = -2
			}

			continue
		}

		if m < dim {
			return nil, 0, fmt.Errorf("%w: dimension %d has size %d above its maximum %d", utils.ErrCorrupt, i, dim, m)
		}

		c := d.layout.ChunkSize[i]
		maxChunks[i] = m / c
		if m%c != 0 {
			maxChunks[i]++
		}
	}

	return maxChunks, unlimited, nil
}

// chunkLinearizer maps the linear index a fixed or extensible array stores a
// chunk under back to its chunk coordinates. The array index is row-major over
// the maximum grid, with the unlimited dimension moved to the front: that is
// H5VM_array_offset_pre over max_down_chunks for the fixed array
// (H5Dfarray.c:1194-1240) and H5VM_chunk_index over swizzled_max_down_chunks
// for the extensible array (H5Dearray.c:1158-1176, :1275-1297). Moving
// dimension 0 to the front changes nothing, so one mapping serves both.
type chunkLinearizer struct {
	down  []uint64 // Row-major strides over the swizzled maximum grid.
	unlim int      // Dimension that was moved to the front.
	total uint64   // Linear indexes the grid holds; math.MaxUint64 when unbounded.
}

// newChunkLinearizer builds the mapping for the maximum grid maxChunks, whose
// dimension unlim (0 when there is none) is moved to the front. Strides that
// overflow make the layout corrupt: C computes them in hsize_t and would wrap.
func newChunkLinearizer(maxChunks []uint64, unlim int) (*chunkLinearizer, error) {
	rank := len(maxChunks)
	swz := make([]uint64, 0, rank)
	swz = append(swz, maxChunks[unlim])
	for i, m := range maxChunks {
		if i != unlim {
			swz = append(swz, m)
		}
	}

	l := &chunkLinearizer{down: make([]uint64, rank), unlim: unlim}
	acc := uint64(1)
	for i := rank - 1; i >= 0; i-- {
		l.down[i] = acc
		var err error
		if acc, err = utils.SafeMultiply(acc, swz[i]); err != nil {
			if i == 0 {
				acc = math.MaxUint64 // Only the unlimited front dimension may be unbounded.
				break
			}
			return nil, fmt.Errorf("%w: maximum chunk grid %v overflows", utils.ErrCorrupt, maxChunks)
		}
	}
	l.total = acc

	return l, nil
}

// coords writes the chunk coordinates of linear index idx into scaled.
func (l *chunkLinearizer) coords(idx uint64, scaled []uint64) {
	rank := len(l.down)
	// Swizzled coordinates first: s[0] is the unlimited (front) dimension.
	front := idx / l.down[0]
	idx %= l.down[0]
	pos := 0
	for i := 0; i < rank; i++ {
		if i == l.unlim {
			scaled[i] = front
			continue
		}
		pos++
		scaled[i] = idx / l.down[pos]
		idx %= l.down[pos]
	}
}

// index is the inverse of coords: the linear array index of the chunk at
// scaled, which lies inside the maximum grid.
func (l *chunkLinearizer) index(scaled []uint64) uint64 {
	idx := scaled[l.unlim] * l.down[0]
	pos := 0
	for i, s := range scaled {
		if i == l.unlim {
			continue
		}
		pos++
		idx += s * l.down[pos]
	}

	return idx
}

// chunkSizeLen is the width of a filtered chunk's stored size in the fixed
// array, extensible array and v2 B-tree records. Version 4 uses one byte more
// than the unfiltered chunk size needs, at most 8; later versions use the
// file's length size (H5D_FARRAY_FILT_COMPUTE_CHUNK_SIZE_LEN, H5Dfarray.c:55-63;
// the same macro in H5Dearray.c:57-65 and H5Dbtree2.c:48-56).
func (d *DatasetDescriptor) chunkSizeLen(sb *Superblock) int {
	if d.layout.Version > 4 {
		return int(sb.LengthSize)
	}

	n := 1 + (bits.Len64(d.chunkBytes)-1+8)/8 // H5VM_log2_gen(size) = bit length - 1.

	return min(n, 8)
}

// chunkElemCodec decodes the chunk records the fixed and extensible arrays
// store: the address alone, or for a filtered dataset the address, the stored
// size in chunkSizeLen bytes and the 32-bit filter mask
// (H5D__farray_filt_decode, H5Dfarray.c:582-610; H5D__earray_filt_decode,
// H5Dearray.c:513-540).
type chunkElemCodec struct {
	sb         *Superblock
	filtered   bool
	sizeLen    int
	chunkBytes uint64
}

func newChunkElemCodec(d *DatasetDescriptor, sb *Superblock) chunkElemCodec {
	c := chunkElemCodec{sb: sb, filtered: d.filtered(), chunkBytes: d.chunkBytes}
	if c.filtered {
		c.sizeLen = d.chunkSizeLen(sb)
	}

	return c
}

// rawSize is the encoded record size (H5Dfarray.c:838-846, H5Dearray.c:879-887).
func (c chunkElemCodec) rawSize() uint64 {
	n := uint64(c.sb.OffsetSize)
	if c.filtered {
		n += uint64(c.sizeLen) + 4 //nolint:gosec // G115: chunkSizeLen is 1 to 8 bytes
	}

	return n
}

// decode returns the chunk address, stored size and filter mask of one record.
// An undefined address is a chunk never written.
func (c chunkElemCodec) decode(raw []byte) (addr, nbytes uint64, mask uint32) {
	os := int(c.sb.OffsetSize)
	addr = readLayoutAddress(raw, os, c.sb.Endianness)
	if !c.filtered {
		return addr, c.chunkBytes, 0
	}

	nbytes = readUint64(raw[os:], c.sizeLen, binary.LittleEndian)
	mask = binary.LittleEndian.Uint32(raw[os+c.sizeLen:])

	return addr, nbytes, mask
}

// maxIndexBlock caps one index metadata block (an array data block, a super
// block, a v2 B-tree node). The file bound applies first; this keeps a reader
// of unknown size from allocating more than the ADR-003 chunk cap at once.
const maxIndexBlock = utils.MaxChunkSize

// readChecksummedBlock reads size bytes at addr and verifies the Jenkins
// lookup3 checksum in the last four bytes against the bytes before them, as
// H5F_get_checksums (H5Fio.c:499) does for every fixed and extensible array block
// (called from H5FAcache.c and H5EAcache.c). Every failure wraps
// utils.ErrCorrupt.
func readChecksummedBlock(r io.ReaderAt, sb *Superblock, addr, size uint64, what string) ([]byte, error) {
	if size < 4 {
		return nil, fmt.Errorf("%w: %s at 0x%X: %d bytes cannot hold a checksum", utils.ErrCorrupt, what, addr, size)
	}

	buf, err := ReadBlock(r, sb, addr, size, maxIndexBlock, what)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", utils.ErrCorrupt, err)
	}

	n := len(buf) - 4
	if stored, computed := binary.LittleEndian.Uint32(buf[n:]), JenkinsChecksum(buf[:n]); stored != computed {
		return nil, fmt.Errorf("%w: %s at 0x%X: checksum mismatch (stored 0x%08X, computed 0x%08X)",
			utils.ErrCorrupt, what, addr, stored, computed)
	}

	return buf, nil
}

// checkBlockPrefix verifies the signature, version 0 and class byte that open
// every fixed and extensible array block (H5FAcache.c, H5EAcache.c
// deserialize callbacks), and the owning header address that follows them in
// every block but the header.
func checkBlockPrefix(buf []byte, sig string, class uint8, hdrAddr uint64, sb *Superblock, what string) error {
	if string(buf[:4]) != sig {
		return fmt.Errorf("%w: %s: wrong signature %q, want %q", utils.ErrCorrupt, what, buf[:4], sig)
	}
	if buf[4] != 0 {
		return fmt.Errorf("%w: %s: unsupported version %d", utils.ErrCorrupt, what, buf[4])
	}
	if buf[5] != class {
		return fmt.Errorf("%w: %s: array class %d, the header says %d", utils.ErrCorrupt, what, buf[5], class)
	}
	if got := readLayoutAddress(buf[6:], int(sb.OffsetSize), sb.Endianness); got != hdrAddr {
		return fmt.Errorf("%w: %s: header address 0x%X, want 0x%X", utils.ErrCorrupt, what, got, hdrAddr)
	}

	return nil
}

// pageInitBit is H5VM_bit_get: bit i of the bitmap, most significant first
// (H5VMprivate.h:453-480).
func pageInitBit(bitmap []byte, i uint64) bool {
	return bitmap[i/8]&(0x80>>(i%8)) != 0
}
