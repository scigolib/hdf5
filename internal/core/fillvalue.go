package core

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/scigolib/hdf5/internal/utils"
)

// Fill value write times, H5D_fill_time_t (H5Dpublic.h:100-102).
const (
	FillTimeAlloc uint8 = 0 // Fill on allocation.
	FillTimeNever uint8 = 1 // Never write fill values.
	FillTimeIfSet uint8 = 2 // Fill if the fill value was set.
)

// Space allocation times, H5D_alloc_time_t (H5Dpublic.h:74-77).
const (
	AllocTimeDefault uint8 = 0
	AllocTimeEarly   uint8 = 1
	AllocTimeLate    uint8 = 2
	AllocTimeIncr    uint8 = 3
)

// Fill value message, version 3 flags (H5Ofill.c:161-169).
const (
	fillMaskAllocTime     = 0x03
	fillShiftAllocTime    = 0
	fillMaskFillTime      = 0x03
	fillShiftFillTime     = 2
	fillFlagUndefined     = 0x10
	fillFlagHaveValue     = 0x20
	fillFlagsAll          = fillMaskAllocTime<<fillShiftAllocTime | fillMaskFillTime<<fillShiftFillTime | fillFlagUndefined | fillFlagHaveValue
	fillVersionLatest     = 3
	fillSizeUndefined     = -1
	fillMessageHeaderSize = 4 // Size field of the old message and of a defined value.
)

// FillStatus is H5D_fill_value_t as H5P_is_fill_value_defined computes it
// (H5Pdcpl.c:3309-3334).
type FillStatus uint8

// Fill value states.
const (
	FillUndefined   FillStatus = iota // No fill value (size -1): C refuses some reads.
	FillDefault                       // The library default: zero bytes.
	FillUserDefined                   // A value stored in the message.
	fillInvalid                       // A combination C reports as an error when used.
)

// FillValueMessage is a dataset's fill value, decoded from the fill value
// message 0x0005 (H5O__fill_new_decode, H5Ofill.c:189) or, when that is
// absent, the old message 0x0004 (H5O__fill_old_decode, H5Ofill.c:328), with
// the defaults H5D__open_oid sets (H5Dint.c:1779-1829).
type FillValueMessage struct {
	Version   uint8
	AllocTime uint8
	FillTime  uint8

	// Size is C's fill->size: -1 for an undefined value, 0 for the library
	// default (zeros), the byte size of Value otherwise.
	Size int64

	// Value holds the stored bytes in the file's representation of the
	// dataset's datatype; nil unless the message stores one.
	Value []byte
}

// defaultFillValue is the fill value of a dataset without a fill value
// message. H5D__open_oid starts from the DCPL default (size 0, fill time
// IFSET, H5Pdcpl.c:161-165), sets the allocation time by layout and then maps
// the size 0 to -1 (H5Dint.c:1799-1827): the value is undefined, not the
// zero default, so unallocated contiguous storage cannot be read and missing
// chunks are skipped.
func defaultFillValue(layoutClass DataLayoutClass) *FillValueMessage {
	f := &FillValueMessage{Version: 2, FillTime: FillTimeIfSet, Size: fillSizeUndefined}
	switch layoutClass {
	case LayoutCompact:
		f.AllocTime = AllocTimeEarly
	case LayoutChunked, LayoutVirtual:
		f.AllocTime = AllocTimeIncr
	default:
		f.AllocTime = AllocTimeLate
	}
	return f
}

// Status reports how the fill value is defined (H5P_is_fill_value_defined).
func (f *FillValueMessage) Status() FillStatus {
	switch {
	case f.Size == fillSizeUndefined && f.Value == nil:
		return FillUndefined
	case f.Size == 0 && f.Value == nil:
		return FillDefault
	case f.Size > 0 && f.Value != nil:
		return FillUserDefined
	default:
		return fillInvalid
	}
}

// ParseFillValueMessage decodes a fill value message 0x0005, versions 1-3, as
// H5O__fill_new_decode does (H5Ofill.c:189-310). Every read is bounds-checked;
// bad bytes wrap utils.ErrCorrupt. The value is copied out of data.
func ParseFillValueMessage(data []byte) (*FillValueMessage, error) {
	if len(data) < 1 {
		return nil, fmt.Errorf("%w: fill value message is empty", utils.ErrCorrupt)
	}

	f := &FillValueMessage{Version: data[0]}
	if f.Version < 1 || f.Version > fillVersionLatest {
		return nil, fmt.Errorf("%w: bad version number %d for fill value message", utils.ErrCorrupt, f.Version)
	}
	p := data[1:]

	var err error
	if f.Version < fillVersionLatest {
		err = f.decodeV1V2(p)
	} else {
		err = f.decodeV3(p)
	}
	if err != nil {
		return nil, err
	}
	return f, nil
}

// decodeV1V2 decodes the body of a version 1 or 2 message: allocation time,
// fill time, a defined flag and, when defined, an INT32 size and the value.
func (f *FillValueMessage) decodeV1V2(p []byte) error {
	if len(p) < 3 {
		return fmt.Errorf("%w: fill value message version %d is truncated", utils.ErrCorrupt, f.Version)
	}
	f.AllocTime, f.FillTime = p[0], p[1]
	defined := p[2] != 0
	p = p[3:]

	if !defined {
		f.Size = fillSizeUndefined
		return nil
	}

	if len(p) < fillMessageHeaderSize {
		return fmt.Errorf("%w: fill value message has no value size", utils.ErrCorrupt)
	}
	// INT32DECODE: a signed size; zero or negative stores no value.
	f.Size = int64(int32(binary.LittleEndian.Uint32(p))) //nolint:gosec // G115: C decodes it as int32.
	if f.Size <= 0 {
		return nil
	}
	return f.copyValue(p[fillMessageHeaderSize:])
}

// decodeV3 decodes the body of a version 3 message: a flags byte and, when
// the value flag is set, a UINT32 size and the value.
func (f *FillValueMessage) decodeV3(p []byte) error {
	if len(p) < 1 {
		return fmt.Errorf("%w: fill value message has no flags", utils.ErrCorrupt)
	}
	flags := p[0]
	p = p[1:]

	if flags&^fillFlagsAll != 0 {
		return fmt.Errorf("%w: unknown flags 0x%02x in fill value message", utils.ErrCorrupt, flags)
	}
	f.AllocTime = (flags >> fillShiftAllocTime) & fillMaskAllocTime
	f.FillTime = (flags >> fillShiftFillTime) & fillMaskFillTime

	switch {
	case flags&fillFlagUndefined != 0:
		if flags&fillFlagHaveValue != 0 {
			return fmt.Errorf("%w: fill value message has both the value and the undefined flag", utils.ErrCorrupt)
		}
		f.Size = fillSizeUndefined
	case flags&fillFlagHaveValue != 0:
		if len(p) < fillMessageHeaderSize {
			return fmt.Errorf("%w: fill value message has no value size", utils.ErrCorrupt)
		}
		f.Size = int64(binary.LittleEndian.Uint32(p))
		if err := f.copyValue(p[fillMessageHeaderSize:]); err != nil {
			return err
		}
		// A zero size leaves C with a zero-byte buffer, which it reports as an
		// invalid combination when the value is used (Status: fillInvalid).
		if f.Size == 0 {
			f.Value = []byte{}
		}
	}

	return nil
}

// ParseFillValueOldMessage decodes the old fill value message 0x0004 as
// H5O__fill_old_decode does (H5Ofill.c:328-392): a 4-byte size and the value,
// which must have the dataset's datatype size (H5Ofill.c:369-371). A size of
// zero is an undefined value (H5Dint.c:1825-1827).
func ParseFillValueOldMessage(data []byte, datatypeSize uint32) (*FillValueMessage, error) {
	f := &FillValueMessage{Version: 2, AllocTime: AllocTimeLate, FillTime: FillTimeIfSet}

	if len(data) < fillMessageHeaderSize {
		return nil, fmt.Errorf("%w: old fill value message has no value size", utils.ErrCorrupt)
	}
	size := binary.LittleEndian.Uint32(data)
	if size == 0 {
		f.Size = fillSizeUndefined
		return f, nil
	}

	f.Size = int64(size)
	if err := f.copyValue(data[fillMessageHeaderSize:]); err != nil {
		return nil, err
	}
	if size != datatypeSize {
		return nil, fmt.Errorf("%w: old fill value size %d does not match the datatype size %d",
			utils.ErrCorrupt, size, datatypeSize)
	}

	return f, nil
}

// copyValue copies f.Size bytes from p; the message must hold them.
func (f *FillValueMessage) copyValue(p []byte) error {
	if f.Size > int64(len(p)) {
		return fmt.Errorf("%w: fill value of %d bytes exceeds the %d bytes left in its message",
			utils.ErrCorrupt, f.Size, len(p))
	}
	if f.Size > 0 {
		f.Value = append([]byte(nil), p[:f.Size]...)
	}
	return nil
}

// errFillUndefined is C's refusal to read unallocated contiguous storage whose
// fill value is undefined while fill values would be written (H5Dio.c:256-260).
// The file is valid; there is just no data to read.
var errFillUndefined = errors.New("dataset storage is not allocated and its fill value is undefined: no data can be read")

// FillUnallocated writes the dataset's fill value into dst, which stands for
// storage the file does not have: unallocated contiguous storage, or the
// chunks of a chunked dataset that were never written. dst must hold whole
// elements and be zeroed; it is the one place every reader fills from.
//
// It follows C:
//   - contiguous (H5D__read, H5Dio.c:239-279): an undefined fill value fails
//     the read unless fill values are never written; fill time "never" leaves
//     the buffer as it is (zeros here, C leaves the caller's bytes);
//   - chunked (H5D__chunk_read, H5Dchunk.c:2936-2949): missing chunks are
//     skipped (zeros) for fill time "never" and for an undefined value when
//     filling only if set;
//   - otherwise a user-defined value is repeated, the default is zeros
//     (H5D__fill, H5Dfill.c:109).
//
// A user-defined value of a datatype with variable-length parts is stored as
// heap references that would each need their own global heap object; that is
// refused with utils.ErrNotSupported (C converts it per element,
// H5D__fill_refill_vl, H5Dfill.c).
func (d *DatasetDescriptor) FillUnallocated(dst []byte) error {
	f := d.fill
	if len(dst) == 0 || f.FillTime == FillTimeNever {
		return nil
	}

	switch f.Status() {
	case FillDefault:
		return nil
	case FillUndefined:
		if d.layout.Class == LayoutChunked {
			return nil // ALLOC fills zeros (H5D__fill with no buffer); IFSET skips.
		}
		return errFillUndefined
	case FillUserDefined:
	default:
		return fmt.Errorf("%w: fill value has size %d and %d stored bytes (H5Pdcpl.c:3327-3329)",
			utils.ErrCorrupt, f.Size, len(f.Value))
	}

	if containsVarLen(d.datatype) {
		return fmt.Errorf("fill value of a variable-length datatype: %w", utils.ErrNotSupported)
	}
	// C copies dt_size bytes from the value; a shorter one would be read past.
	if uint64(f.Size) < d.elemSize { //nolint:gosec // G115: a user-defined value has Size > 0 (Status).
		return fmt.Errorf("%w: fill value of %d bytes is shorter than the %d-byte datatype",
			utils.ErrCorrupt, f.Size, d.elemSize)
	}
	if uint64(len(dst))%d.elemSize != 0 {
		return fmt.Errorf("fill buffer of %d bytes does not hold whole %d-byte elements", len(dst), d.elemSize)
	}

	// Doubling copies: O(log n) calls for any buffer size.
	n := copy(dst, f.Value[:d.elemSize])
	for n < len(dst) {
		n += copy(dst[n:], dst[:n])
	}
	return nil
}

// ChunksMissing reports whether a chunked dataset whose index lists present
// chunks has chunks that were never written. CollectChunks keeps every key
// inside the grid, so fewer entries than grid cells means a missing chunk.
func (d *DatasetDescriptor) ChunksMissing(present int) bool {
	total := uint64(1)
	for _, n := range d.ChunkGrid() {
		var err error
		if total, err = utils.SafeMultiply(total, n); err != nil {
			return true
		}
	}
	return uint64(present) < total //nolint:gosec // G115: a slice length is never negative.
}

// FillValue returns the dataset's fill value: the decoded fill value message,
// or the library default when the dataset has none.
func (d *DatasetDescriptor) FillValue() *FillValueMessage { return d.fill }

// containsVarLen reports whether dt has a variable-length part anywhere.
func containsVarLen(dt *DatatypeMessage) bool {
	if dt == nil {
		return false
	}
	switch dt.Class {
	case DatatypeVarLen:
		return true
	case DatatypeCompound:
		for i := range dt.Members {
			if containsVarLen(dt.Members[i].Type) {
				return true
			}
		}
		return false
	default:
		return containsVarLen(dt.Base)
	}
}
