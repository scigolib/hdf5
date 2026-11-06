package core

import (
	"encoding/binary"
	"fmt"
)

// Fill value message versions (H5Oprivate.h:314-322).
const (
	fillVersion1 = 1
	fillVersion2 = 2
	fillVersion3 = 3
)

// NewDatasetFillValue returns the fill value message H5Dcreate writes for a
// dataset of datatype dt and layout layoutClass created with the default
// dataset creation property list, in a file whose superblock has version
// superblockVersion:
//
//   - the default property is version 2, fill time IFSET and the library
//     default value (size 0, H5Pdcpl.c:161-165);
//   - the allocation time follows the layout (H5Pdcpl.c:1856-1876): early for
//     compact, late for contiguous, incremental for chunked and virtual;
//   - a datatype with a variable-length part gets fill time ALLOC
//     (H5Dint.c:928-937, H5T_detect_class counts a variable-length string as
//     variable-length when not asked from the API, H5T.c:2876-2882);
//   - H5O_fill_set_version raises the version to the file's low bound
//     (H5Dint.c:1326, H5Ofill.c:1036): version 3 from the 1.8 format on,
//     which a version 2 or 3 superblock implies; a version 0 or 1 superblock
//     is the earliest format, where version 2 stays (H5Ofill.c:150-158).
func NewDatasetFillValue(dt *DatatypeMessage, layoutClass DataLayoutClass, superblockVersion uint8) *FillValueMessage {
	f := &FillValueMessage{Version: fillVersion2, FillTime: FillTimeIfSet, Size: 0}
	if superblockVersion >= Version2 {
		f.Version = fillVersion3
	}
	switch layoutClass {
	case LayoutCompact:
		f.AllocTime = AllocTimeEarly
	case LayoutChunked, LayoutVirtual:
		f.AllocTime = AllocTimeIncr
	default:
		f.AllocTime = AllocTimeLate
	}
	if containsVarLen(dt) {
		f.FillTime = FillTimeAlloc
	}
	return f
}

// EncodeFillValueMessage encodes f as the fill value message 0x0005 the way
// H5O__fill_new_encode does (H5Ofill.c:409-490):
//
//   - versions 1 and 2: allocation time, fill time, a "defined" byte and,
//     when defined, a UINT32 size followed by the value;
//   - version 3: one flags byte (allocation time in bits 0-1, fill time in
//     bits 2-3, 0x10 for an undefined value, 0x20 when a value follows) and,
//     with a value, an INT32 size and the value.
//
// Size -1 is an undefined value, 0 the library default (zeros), anything
// else must be len(Value).
func EncodeFillValueMessage(f *FillValueMessage) ([]byte, error) {
	if err := validateFillValueMessage(f); err != nil {
		return nil, err
	}

	if f.Version < fillVersion3 {
		defined := f.Size >= 0
		buf := []byte{f.Version, f.AllocTime, f.FillTime, 0}
		if !defined {
			return buf, nil
		}
		buf[3] = 1
		buf = binary.LittleEndian.AppendUint32(buf, uint32(f.Size)) //nolint:gosec // G115: 0 <= Size <= 0x7FFFFFFF, checked by validateFillValueMessage
		return append(buf, f.Value...), nil
	}

	flags := f.AllocTime<<fillShiftAllocTime | f.FillTime<<fillShiftFillTime
	switch {
	case f.Size < 0:
		return []byte{f.Version, flags | fillFlagUndefined}, nil
	case f.Size == 0:
		return []byte{f.Version, flags}, nil
	default:
		buf := []byte{f.Version, flags | fillFlagHaveValue}
		buf = binary.LittleEndian.AppendUint32(buf, uint32(f.Size)) //nolint:gosec // G115: 0 < Size <= 0x7FFFFFFF, checked by validateFillValueMessage
		return append(buf, f.Value...), nil
	}
}

// validateFillValueMessage rejects a message EncodeFillValueMessage cannot
// write: an unknown version, allocation or fill time outside its 2-bit field,
// a size other than -1, 0 or len(Value), or one too large for the 32-bit size
// field the C encoder writes (H5Ofill.c:434, 473).
func validateFillValueMessage(f *FillValueMessage) error {
	if f == nil {
		return fmt.Errorf("fill value message is nil")
	}
	if f.Version < fillVersion1 || f.Version > fillVersion3 {
		return fmt.Errorf("fill value message version %d is not 1, 2 or 3", f.Version)
	}
	if f.AllocTime&^fillMaskAllocTime != 0 || f.FillTime&^fillMaskFillTime != 0 {
		return fmt.Errorf("fill value allocation time %d or fill time %d out of range", f.AllocTime, f.FillTime)
	}
	switch {
	case f.Size < fillSizeUndefined:
		return fmt.Errorf("fill value size %d is invalid", f.Size)
	case f.Size <= 0 && len(f.Value) != 0:
		return fmt.Errorf("fill value of size %d holds %d bytes", f.Size, len(f.Value))
	case f.Size > 0 && int64(len(f.Value)) != f.Size:
		return fmt.Errorf("fill value size %d does not match its %d bytes", f.Size, len(f.Value))
	case f.Size > 0x7FFFFFFF:
		return fmt.Errorf("fill value size %d exceeds the 32-bit size field", f.Size)
	}
	return nil
}
