package hdf5

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/structures"
	"github.com/scigolib/hdf5/internal/writer"
)

// errAppending is returned by Write, WriteRaw and Resize on a dataset that rows have been appended to.
var errAppending = errors.New("dataset is being appended to: use AppendRows and Flush, not Write or Resize")

// appender holds what AppendRows keeps between calls.
//
// Rows are appended along the first dimension. A slab is one chunk deep in that dimension and spans the
// others, so a complete slab is a set of complete chunks that never change again. Rows of the slab being
// filled wait in pending until it is complete, or until Flush writes them as chunks padded with zeros.
type appender struct {
	rowElems  uint64 // elements in one row: the product of every dimension but the first
	rowBytes  uint64 // bytes in one row
	slabRows  uint64 // rows in a slab: the chunk size in the first dimension
	slabDims  []uint64
	paddedDim []uint64 // slabDims rounded up to whole chunks in every dimension
	padBytes  uint64   // bytes in a slab of paddedDim
	coord     *writer.ChunkCoordinator

	rows    uint64 // rows appended so far
	slabs   uint64 // complete slabs written
	pending []byte // rows of the incomplete slab

	entries []structures.ChunkBTreeEntry // the complete slabs' chunks
	partial []structures.ChunkBTreeEntry // the incomplete slab's chunks, as last flushed

	index []block // file space of the chunk index last written, freed when it is replaced
	dirty bool    // rows appended since the last Flush

	// failed is the error of an append that stopped partway, after which the slabs written no longer
	// match rows. Every later AppendRows, AppendRawRows and Flush returns it.
	failed error
}

// block is a range of file space.
type block struct{ addr, size uint64 }

// recordingAllocator allocates from a file and remembers what it allocated.
type recordingAllocator struct {
	fw     *writer.FileWriter
	blocks []block
}

func (r *recordingAllocator) Allocate(size uint64) (uint64, error) {
	addr, err := r.fw.Allocate(size)
	if err == nil {
		r.blocks = append(r.blocks, block{addr, size})
	}
	return addr, err
}

// AppendRows adds rows to the end of the dataset's first dimension.
//
// The dataset must be chunked, created with a first dimension of 0 whose maximum is Unlimited, for
// example with dims {0, n}, WithMaxDims({Unlimited, n}) and WithChunkDims. data is a slice of the
// dataset's integer or floating-point element type, flattened in row-major order, holding a whole
// number of rows.
//
// Each complete chunk is written to the file once, when the rows that fill it arrive, so the cost of an
// append does not depend on how large the dataset already is. What has been appended is readable in
// the file after Flush, or after FileWriter.Close, which flushes every dataset that has been appended to.
//
// Once rows have been appended, Write, WriteRaw and Resize refuse the dataset.
//
// An append that fails while writing to the file leaves the dataset unusable for appending: that call,
// and every later AppendRows, AppendRawRows, Flush and Close of the dataset, returns the error. The rows
// flushed before it stay readable. An append refused before anything is written, such as one that is not
// a whole number of rows, does not.
func (dw *DatasetWriter) AppendRows(data interface{}) error {
	if dw.dtype == nil {
		return errCommittedDatatypeData
	}
	n, err := sliceLen(data)
	if err != nil {
		return err
	}
	size := uint64(n) * uint64(dw.elementSize) //nolint:gosec // G115: a slice length is never negative

	var buf []byte
	switch dw.dtype.Class {
	case core.DatatypeFixed:
		buf, err = encodeFixedPointData(data, dw.dtype.Size, size)
	case core.DatatypeFloat:
		buf, err = encodeFloatData(data, dw.dtype.Size, size)
	default:
		return fmt.Errorf("AppendRows supports integer and floating-point datasets, not class %d", dw.dtype.Class)
	}
	if err != nil {
		return fmt.Errorf("failed to encode data: %w", err)
	}
	return dw.AppendRawRows(buf)
}

// AppendRawRows is AppendRows for data already encoded as the dataset's elements are stored.
func (dw *DatasetWriter) AppendRawRows(buf []byte) error {
	a, err := dw.startAppending()
	if err != nil {
		return err
	}
	if a.failed != nil {
		return a.failed
	}
	if uint64(len(buf))%a.rowBytes != 0 {
		return fmt.Errorf("%d bytes is not a whole number of %d-byte rows", len(buf), a.rowBytes)
	}
	newRows := a.rows + uint64(len(buf))/a.rowBytes

	for len(buf) > 0 {
		took, err := dw.appendSome(a, buf)
		if err != nil {
			a.failed = fmt.Errorf("dataset %q: an append failed partway, so it can no longer be appended to: %w",
				dw.name, err)
			return a.failed
		}
		buf = buf[took:]
	}
	a.rows = newRows
	a.dirty = true
	return nil
}

// appendSome writes or buffers the start of buf, and returns how many of its bytes it took.
//
// Whole slabs at the start of buf are written straight from it, several in one write when each slab is a
// single unfiltered chunk. Rows that don't complete a slab wait in pending.
func (dw *DatasetWriter) appendSome(a *appender, buf []byte) (uint64, error) {
	slabBytes := a.slabRows * a.rowBytes
	if len(a.pending) == 0 {
		k := uint64(len(buf)) / slabBytes
		if k > 1 && dw.slabIsOneRawChunk(a) {
			return k * slabBytes, dw.writeContiguousSlabs(a, buf[:k*slabBytes], k)
		}
		if k == 1 {
			return slabBytes, dw.writeSlab(a, buf[:slabBytes])
		}
	}
	take := min(slabBytes-uint64(len(a.pending)), uint64(len(buf)))
	a.pending = append(a.pending, buf[:take]...)
	if uint64(len(a.pending)) < slabBytes {
		return take, nil
	}
	err := dw.writeSlab(a, a.pending)
	a.pending = a.pending[:0]
	return take, err
}

// Flush makes the rows appended so far readable in the file: it writes the incomplete slab, the dataset's
// dimensions, the chunk index and the end-of-file address. A dataset nothing has been appended to since
// the last Flush is left as it is.
//
// After Flush returns, a reader sees every row appended before it, even if the writing process stops
// without closing the file. Flush does not wait for the data to reach the disk.
func (dw *DatasetWriter) Flush() error {
	a := dw.appender
	if a == nil {
		return nil
	}
	if a.failed != nil {
		return a.failed
	}
	if !a.dirty {
		return nil
	}
	fw := dw.fileWriter

	if err := dw.writePartialSlab(a); err != nil {
		return err
	}

	newDims := append([]uint64{a.rows}, dw.dims[1:]...)
	if err := dw.setDataspaceDims(newDims); err != nil {
		return err
	}

	entries := make([]structures.ChunkBTreeEntry, 0, len(a.entries)+len(a.partial))
	entries = append(entries, a.entries...)
	entries = append(entries, a.partial...)
	index := structures.NewChunkBTreeWriter(len(dw.dims), dw.chunkDims, dw.elementSize)
	for _, e := range entries {
		if err := index.AddChunkWithSize(e.Coordinate, e.Address, e.Nbytes); err != nil {
			return err
		}
	}
	rec := &recordingAllocator{fw: fw.writer}
	indexAddr, err := index.WriteToFile(fw.writer, rec)
	if err != nil {
		return fmt.Errorf("failed to write chunk index: %w", err)
	}
	if err := dw.setChunkIndexAddress(indexAddr); err != nil {
		return err
	}
	dw.dataAddress = indexAddr

	// The new index is in place, so the one it replaces can go.
	for _, b := range a.index {
		if err := fw.writer.Free(b.addr, b.size); err != nil {
			return fmt.Errorf("free replaced chunk index: %w", err)
		}
	}
	a.index = rec.blocks

	if err := fw.writeEndOfFile(); err != nil {
		return err
	}
	a.dirty = false
	return nil
}

// startAppending returns the dataset's appender, making it on the first call.
func (dw *DatasetWriter) startAppending() (*appender, error) {
	if dw.appender != nil {
		return dw.appender, nil
	}
	if !dw.isChunked {
		return nil, fmt.Errorf("AppendRows requires chunked layout")
	}
	if len(dw.maxDims) == 0 || dw.maxDims[0] != Unlimited {
		return nil, fmt.Errorf("AppendRows requires a first dimension whose maximum is Unlimited (WithMaxDims)")
	}
	if dw.dims[0] != 0 {
		return nil, fmt.Errorf("AppendRows requires a dataset created with a first dimension of 0, not %d", dw.dims[0])
	}
	for i, d := range dw.dims[1:] {
		if d == 0 {
			return nil, fmt.Errorf("dimension %d is 0: only the first dimension can grow by appending", i+1)
		}
	}

	a := &appender{slabRows: dw.chunkDims[0]}
	a.rowElems = 1
	for _, d := range dw.dims[1:] {
		a.rowElems *= d
	}
	a.rowBytes = a.rowElems * uint64(dw.elementSize)
	a.slabDims = append([]uint64{a.slabRows}, dw.dims[1:]...)
	a.paddedDim = make([]uint64, len(a.slabDims))
	for i, d := range a.slabDims {
		c := dw.chunkDims[i]
		a.paddedDim[i] = (d + c - 1) / c * c
	}
	// The sizes come from the dataset this writer created, not from file metadata, and are checked for
	// overflow here once.
	padBytes, err := datasetByteSize(a.paddedDim, dw.elementSize)
	if err != nil {
		return nil, err
	}
	a.padBytes = padBytes
	coord, err := writer.NewChunkCoordinator(a.paddedDim, dw.chunkDims)
	if err != nil {
		return nil, err
	}
	a.coord = coord
	dw.appender = a
	dw.fileWriter.appenders = append(dw.fileWriter.appenders, dw)
	return a, nil
}

// writeSlab writes one complete slab as chunks and adds them to the index. Space a flushed partial copy
// of the same slab held is freed.
func (dw *DatasetWriter) writeSlab(a *appender, slab []byte) error {
	entries, err := dw.writeSlabChunks(a, slab, a.slabs)
	if err != nil {
		return err
	}
	if err := dw.freeEntries(a.partial); err != nil {
		return err
	}
	a.partial = nil
	a.entries = append(a.entries, entries...)
	a.slabs++
	return nil
}

// slabIsOneRawChunk reports whether a slab is stored as a single chunk with no filters, so that its bytes
// in the file are exactly its bytes in memory.
func (dw *DatasetWriter) slabIsOneRawChunk(a *appender) bool {
	return a.coord.GetTotalChunks() == 1 && equalDims(a.slabDims, a.paddedDim) &&
		(dw.pipeline == nil || dw.pipeline.IsEmpty())
}

// writeContiguousSlabs writes k complete slabs, each one unfiltered chunk, side by side in one block of
// the file.
func (dw *DatasetWriter) writeContiguousSlabs(a *appender, slabs []byte, k uint64) error {
	fw := dw.fileWriter.writer
	addr, err := fw.Allocate(uint64(len(slabs)))
	if err != nil {
		return fmt.Errorf("failed to allocate chunks: %w", err)
	}
	if err := fw.WriteAtAddress(slabs, addr); err != nil {
		return fmt.Errorf("failed to write chunks: %w", err)
	}
	if err := dw.freeEntries(a.partial); err != nil {
		return err
	}
	a.partial = nil
	size := uint64(len(slabs)) / k
	zeros := make([]uint64, len(dw.dims)-1)
	for i := uint64(0); i < k; i++ {
		a.entries = append(a.entries, structures.ChunkBTreeEntry{
			Coordinate: append([]uint64{a.slabs}, zeros...),
			Address:    addr + i*size,
			Nbytes:     uint32(size), //nolint:gosec // G115: a chunk is below 4 GiB, checked when the dataset was created
		})
		a.slabs++
	}
	return nil
}

// writePartialSlab writes the rows of the incomplete slab as chunks padded with zeros, replacing any
// earlier copy of that slab.
func (dw *DatasetWriter) writePartialSlab(a *appender) error {
	if len(a.pending) == 0 {
		return nil
	}
	slab := make([]byte, a.slabRows*a.rowBytes)
	copy(slab, a.pending)
	entries, err := dw.writeSlabChunks(a, slab, a.slabs)
	if err != nil {
		return err
	}
	if err := dw.freeEntries(a.partial); err != nil {
		return err
	}
	a.partial = entries
	return nil
}

// writeSlabChunks writes slab, which holds slabRows full rows, as the chunks of slab number n.
//
// Every chunk is written whole, as the C library stores an unfiltered chunk at the edge of a dataset:
// elements beyond the dataset's extent are zeros.
func (dw *DatasetWriter) writeSlabChunks(a *appender, slab []byte, n uint64) ([]structures.ChunkBTreeEntry, error) {
	fw := dw.fileWriter.writer
	padded := slab
	if !equalDims(a.slabDims, a.paddedDim) {
		padded = padSlab(slab, a.slabDims, a.paddedDim, dw.elementSize, a.padBytes)
	}

	total := a.coord.GetTotalChunks()
	entries := make([]structures.ChunkBTreeEntry, 0, total)
	for i := uint64(0); i < total; i++ {
		c := a.coord.GetChunkCoordinate(i)
		data := padded
		if total > 1 {
			data = a.coord.ExtractChunkData(padded, c, dw.elementSize)
		}
		if dw.pipeline != nil && !dw.pipeline.IsEmpty() {
			filtered, err := dw.pipeline.Apply(data)
			if err != nil {
				return nil, fmt.Errorf("filter application failed for chunk %v: %w", c, err)
			}
			data = filtered
		}
		if uint64(len(data)) > maxV1BTreeChunkSize {
			return nil, fmt.Errorf("chunk %v: filtered size %d bytes must be < 4GB with v1 b-tree index", c, len(data))
		}
		addr, err := fw.Allocate(uint64(len(data)))
		if err != nil {
			return nil, fmt.Errorf("failed to allocate chunk: %w", err)
		}
		if err := fw.WriteAtAddress(data, addr); err != nil {
			return nil, fmt.Errorf("failed to write chunk: %w", err)
		}
		global := append([]uint64{n}, c[1:]...)
		entries = append(entries, structures.ChunkBTreeEntry{
			Coordinate: global,
			Address:    addr,
			Nbytes:     uint32(len(data)), //nolint:gosec // G115: checked against maxV1BTreeChunkSize above
		})
	}
	return entries, nil
}

// freeEntries returns the file space of chunks to the allocator.
func (dw *DatasetWriter) freeEntries(entries []structures.ChunkBTreeEntry) error {
	for _, e := range entries {
		if err := dw.fileWriter.writer.Free(e.Address, uint64(e.Nbytes)); err != nil {
			return fmt.Errorf("free replaced chunk: %w", err)
		}
	}
	return nil
}

// setDataspaceDims records newDims as the dataset's dimensions in its object header.
func (dw *DatasetWriter) setDataspaceDims(newDims []uint64) error {
	fw := dw.fileWriter
	oh, err := core.ReadObjectHeader(fw.writer.Reader(), dw.address, fw.file.sb)
	if err != nil {
		return fmt.Errorf("read object header: %w", err)
	}
	for i, msg := range oh.Messages {
		if msg.Type != core.MsgDataspace {
			continue
		}
		ds, err := core.ParseDataspaceMessage(msg.Data)
		if err != nil {
			return fmt.Errorf("parse dataspace: %w", err)
		}
		data, err := core.EncodeDataspaceMessageVersion(newDims, dw.maxDims, ds.Version)
		if err != nil {
			return fmt.Errorf("encode dataspace: %w", err)
		}
		oh.Messages[i].Data = data
		if err := fw.rewriteObjectHeader(dw.address, oh); err != nil {
			return fmt.Errorf("write object header: %w", err)
		}
		if dw.objectHeader != nil {
			if err := dw.refreshHeaderCache(); err != nil {
				return err
			}
		}
		dw.dims = newDims
		return nil
	}
	return fmt.Errorf("dataspace message not found in object header")
}

// writeEndOfFile records the end of the file in the superblock, so that the file is complete up to this
// point. Its writes reach the operating system, not necessarily the disk: the file survives the writing
// process stopping, as the C library's H5Fflush does, but not the machine losing power.
func (fw *FileWriter) writeEndOfFile() error {
	if fw.file != nil && fw.file.sb != nil {
		if err := fw.file.sb.WriteTo(fw.writer, fw.writer.EndOfFile()); err != nil {
			return fmt.Errorf("failed to update superblock EOA: %w", err)
		}
	}
	return nil
}

// flushAppenders flushes every dataset of the file that rows have been appended to.
func (fw *FileWriter) flushAppenders() error {
	for _, dw := range fw.appenders {
		if err := dw.Flush(); err != nil {
			return fmt.Errorf("dataset %q: %w", dw.name, err)
		}
	}
	return nil
}

// padSlab copies slab, of dimensions dims, into a zero-filled buffer of dimensions padded, which is
// padBytes long.
func padSlab(slab []byte, dims, padded []uint64, elemSize uint32, padBytes uint64) []byte {
	out := make([]byte, padBytes)
	// Copy innermost runs: every index of the outer dimensions of dims.
	inner := dims[len(dims)-1] * uint64(elemSize)
	outer := uint64(1)
	for _, d := range dims[:len(dims)-1] {
		outer *= d
	}
	idx := make([]uint64, len(dims)-1)
	for r := uint64(0); r < outer; r++ {
		var src, dst uint64
		for k := range idx {
			src = src*dims[k] + idx[k]
			dst = dst*padded[k] + idx[k]
		}
		src *= inner
		dst *= padded[len(padded)-1] * uint64(elemSize)
		copy(out[dst:dst+inner], slab[src:src+inner])
		for k := len(idx) - 1; k >= 0; k-- {
			idx[k]++
			if idx[k] < dims[k] {
				break
			}
			idx[k] = 0
		}
	}
	return out
}

func equalDims(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// sliceLen returns the length of a slice, or an error for anything else.
func sliceLen(data interface{}) (int, error) {
	v := reflect.ValueOf(data)
	if v.Kind() != reflect.Slice {
		return 0, fmt.Errorf("data must be a slice, not %T", data)
	}
	return v.Len(), nil
}
