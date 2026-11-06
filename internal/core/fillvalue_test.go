package core

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/scigolib/hdf5/internal/utils"
	"github.com/stretchr/testify/require"
)

// le32 appends v as 4 little-endian bytes.
func le32(b []byte, v uint32) []byte { return binary.LittleEndian.AppendUint32(b, v) }

// TestParseFillValueMessage_LikeC: the new fill value message 0x0005, decoded
// as H5O__fill_new_decode does (H5Ofill.c:189-310).
func TestParseFillValueMessage_LikeC(t *testing.T) {
	cases := []struct {
		name   string
		data   []byte
		want   FillValueMessage
		status FillStatus
	}{
		{
			name:   "v1 undefined",
			data:   []byte{1, AllocTimeLate, FillTimeIfSet, 0},
			want:   FillValueMessage{Version: 1, AllocTime: AllocTimeLate, FillTime: FillTimeIfSet, Size: -1},
			status: FillUndefined,
		},
		{
			name:   "v2 defined with size 0 is the default",
			data:   le32([]byte{2, AllocTimeIncr, FillTimeAlloc, 1}, 0),
			want:   FillValueMessage{Version: 2, AllocTime: AllocTimeIncr, FillTime: FillTimeAlloc},
			status: FillDefault,
		},
		{
			name:   "v2 user value",
			data:   append(le32([]byte{2, AllocTimeEarly, FillTimeIfSet, 1}, 4), 99, 0, 0, 0),
			want:   FillValueMessage{Version: 2, AllocTime: AllocTimeEarly, FillTime: FillTimeIfSet, Size: 4, Value: []byte{99, 0, 0, 0}},
			status: FillUserDefined,
		},
		{
			// INT32DECODE: a negative size stores nothing; C rejects it on use.
			name:   "v2 negative size",
			data:   le32([]byte{2, AllocTimeLate, FillTimeIfSet, 1}, 0xFFFFFFFE),
			want:   FillValueMessage{Version: 2, AllocTime: AllocTimeLate, FillTime: FillTimeIfSet, Size: -2},
			status: fillInvalid,
		},
		{
			name:   "v3 no value flags is the default",
			data:   []byte{3, AllocTimeIncr | FillTimeIfSet<<2},
			want:   FillValueMessage{Version: 3, AllocTime: AllocTimeIncr, FillTime: FillTimeIfSet},
			status: FillDefault,
		},
		{
			name:   "v3 undefined",
			data:   []byte{3, AllocTimeLate | FillTimeNever<<2 | 0x10},
			want:   FillValueMessage{Version: 3, AllocTime: AllocTimeLate, FillTime: FillTimeNever, Size: -1},
			status: FillUndefined,
		},
		{
			name:   "v3 user value",
			data:   append(le32([]byte{3, AllocTimeEarly | FillTimeAlloc<<2 | 0x20}, 2), 0xAB, 0xCD),
			want:   FillValueMessage{Version: 3, AllocTime: AllocTimeEarly, FillTime: FillTimeAlloc, Size: 2, Value: []byte{0xAB, 0xCD}},
			status: FillUserDefined,
		},
		{
			name:   "v3 value of size 0",
			data:   le32([]byte{3, 0x20}, 0),
			want:   FillValueMessage{Version: 3, Value: []byte{}},
			status: fillInvalid,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseFillValueMessage(tc.data)
			require.NoError(t, err)
			require.Equal(t, tc.want, *got)
			require.Equal(t, tc.status, got.Status())
		})
	}
}

// TestParseFillValueMessage_RejectsLikeC: every refusal of
// H5O__fill_new_decode is ErrCorrupt.
func TestParseFillValueMessage_RejectsLikeC(t *testing.T) {
	cases := map[string][]byte{
		"empty":                   {},
		"version 0":               {0, 2, 2, 0},
		"version 4":               {4, 0},
		"v2 truncated":            {2, 2},
		"v2 defined without size": {2, 2, 2, 1, 4, 0},
		"v2 value past the end":   append(le32([]byte{2, 2, 2, 1}, 8), 1, 2, 3),
		"v3 no flags":             {3},
		"v3 unknown flag":         {3, 0x40},
		"v3 both value flags":     le32([]byte{3, 0x30}, 0),
		"v3 value without size":   {3, 0x20, 1},
		"v3 value past the end":   append(le32([]byte{3, 0x20}, 4), 1),
		"v3 huge size, few bytes": append(le32([]byte{3, 0x20}, 0xFFFFFFFF), 1, 2),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseFillValueMessage(data)
			require.Error(t, err)
			require.ErrorIs(t, err, utils.ErrCorrupt)
		})
	}
}

// TestParseFillValueOldMessage_LikeC: the old message 0x0004
// (H5O__fill_old_decode, H5Ofill.c:328-392) with the size check against the
// datatype (H5Ofill.c:369-371) and the open-time defaults.
func TestParseFillValueOldMessage_LikeC(t *testing.T) {
	got, err := ParseFillValueOldMessage(append(le32(nil, 4), 0x5C, 0x11, 0, 0), 4)
	require.NoError(t, err)
	require.Equal(t, FillValueMessage{Version: 2, AllocTime: AllocTimeLate, FillTime: FillTimeIfSet,
		Size: 4, Value: []byte{0x5C, 0x11, 0, 0}}, *got)
	require.Equal(t, FillUserDefined, got.Status())

	got, err = ParseFillValueOldMessage(le32(nil, 0), 4)
	require.NoError(t, err)
	require.Equal(t, FillUndefined, got.Status())

	for name, data := range map[string][]byte{
		"no size":           {4, 0},
		"value past end":    append(le32(nil, 4), 1, 2),
		"datatype mismatch": append(le32(nil, 2), 1, 2),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseFillValueOldMessage(data, 4)
			require.ErrorIs(t, err, utils.ErrCorrupt)
		})
	}
}

// TestDefaultFillValue_Undefined: without a fill message, H5D__open_oid maps
// the default size 0 to -1 (H5Dint.c:1826-1827), an undefined value, with the
// allocation time chosen by layout.
func TestDefaultFillValue_Undefined(t *testing.T) {
	for class, alloc := range map[DataLayoutClass]uint8{
		LayoutCompact:    AllocTimeEarly,
		LayoutContiguous: AllocTimeLate,
		LayoutChunked:    AllocTimeIncr,
	} {
		f := defaultFillValue(class)
		require.Equal(t, FillUndefined, f.Status())
		require.Equal(t, FillTimeIfSet, f.FillTime)
		require.Equal(t, alloc, f.AllocTime)
	}
}

// fillDescriptor is a minimal descriptor of int32 elements for FillUnallocated.
func fillDescriptor(class DataLayoutClass, dt *DatatypeMessage, fill *FillValueMessage) *DatasetDescriptor {
	if dt == nil {
		dt = &DatatypeMessage{Class: DatatypeFixed, Size: 4}
	}
	return &DatasetDescriptor{
		datatype: dt,
		layout:   &DataLayoutMessage{Class: class, DataAddress: undefinedAddress},
		fill:     fill,
		elemSize: uint64(dt.Size),
	}
}

// TestFillUnallocated_LikeC: the read-side rules of H5D__read for unallocated
// contiguous storage (H5Dio.c:239-279) and of H5D__chunk_read for missing
// chunks (H5Dchunk.c:2936-2949).
func TestFillUnallocated_LikeC(t *testing.T) {
	user := func(fillTime uint8) *FillValueMessage {
		return &FillValueMessage{Version: 3, FillTime: fillTime, Size: 4, Value: []byte{7, 0, 0, 0}}
	}
	undefined := func(fillTime uint8) *FillValueMessage {
		return &FillValueMessage{Version: 3, FillTime: fillTime, Size: -1}
	}
	sevens := []byte{7, 0, 0, 0, 7, 0, 0, 0, 7, 0, 0, 0}
	zeros := make([]byte, 12)

	cases := []struct {
		name   string
		class  DataLayoutClass
		fill   *FillValueMessage
		want   []byte
		failed bool
	}{
		{"contiguous user IFSET", LayoutContiguous, user(FillTimeIfSet), sevens, false},
		{"contiguous user ALLOC", LayoutContiguous, user(FillTimeAlloc), sevens, false},
		{"contiguous user NEVER leaves the buffer", LayoutContiguous, user(FillTimeNever), zeros, false},
		{"contiguous default", LayoutContiguous, &FillValueMessage{FillTime: FillTimeIfSet}, zeros, false},
		{"contiguous undefined IFSET fails", LayoutContiguous, undefined(FillTimeIfSet), nil, true},
		{"contiguous undefined ALLOC fails", LayoutContiguous, undefined(FillTimeAlloc), nil, true},
		{"contiguous undefined NEVER", LayoutContiguous, undefined(FillTimeNever), zeros, false},
		{"contiguous without a message fails", LayoutContiguous, defaultFillValue(LayoutContiguous), nil, true},
		{"chunked user IFSET", LayoutChunked, user(FillTimeIfSet), sevens, false},
		{"chunked user NEVER skips", LayoutChunked, user(FillTimeNever), zeros, false},
		{"chunked undefined IFSET skips", LayoutChunked, undefined(FillTimeIfSet), zeros, false},
		{"chunked without a message skips", LayoutChunked, defaultFillValue(LayoutChunked), zeros, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := fillDescriptor(tc.class, nil, tc.fill)
			buf := make([]byte, 12)
			err := d.FillUnallocated(buf)
			if tc.failed {
				require.Error(t, err)
				require.False(t, errors.Is(err, utils.ErrCorrupt), "a valid file: %v", err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, buf)
		})
	}
}

// TestFillUnallocated_Refusals: invalid fill states are ErrCorrupt; a user
// value of a variable-length type is not supported.
func TestFillUnallocated_Refusals(t *testing.T) {
	invalid := &FillValueMessage{Version: 3, FillTime: FillTimeIfSet, Value: []byte{}}
	err := fillDescriptor(LayoutChunked, nil, invalid).FillUnallocated(make([]byte, 4))
	require.ErrorIs(t, err, utils.ErrCorrupt)

	short := &FillValueMessage{Version: 3, FillTime: FillTimeIfSet, Size: 2, Value: []byte{1, 2}}
	err = fillDescriptor(LayoutChunked, nil, short).FillUnallocated(make([]byte, 4))
	require.ErrorIs(t, err, utils.ErrCorrupt)

	vlen := &DatatypeMessage{Class: DatatypeVarLen, Size: 16}
	user := &FillValueMessage{Version: 3, FillTime: FillTimeIfSet, Size: 16, Value: make([]byte, 16)}
	err = fillDescriptor(LayoutChunked, vlen, user).FillUnallocated(make([]byte, 32))
	require.ErrorIs(t, err, utils.ErrNotSupported)

	compound := &DatatypeMessage{Class: DatatypeCompound, Size: 16, Members: []CompoundMember{{Type: vlen}}}
	err = fillDescriptor(LayoutChunked, compound, user).FillUnallocated(make([]byte, 32))
	require.ErrorIs(t, err, utils.ErrNotSupported)

	// The default value of a vlen type is zeros, as C writes it.
	buf := make([]byte, 32)
	require.NoError(t, fillDescriptor(LayoutChunked, vlen, &FillValueMessage{FillTime: FillTimeIfSet}).FillUnallocated(buf))
	require.Equal(t, make([]byte, 32), buf)
}

// TestDescriptor_FillPrecedence: the new message wins over the old one, the
// old one over the default (H5D__open_oid, H5Dint.c:1779-1829).
func TestDescriptor_FillPrecedence(t *testing.T) {
	dt := &DatatypeMessage{Class: DatatypeFixed, Size: 4}
	newMsg := &HeaderMessage{Type: MsgFillValue, Data: append(le32([]byte{3, 0x20 | FillTimeAlloc<<2}, 4), 1, 0, 0, 0)}
	oldMsg := &HeaderMessage{Type: MsgFillValueOld, Data: append(le32(nil, 4), 2, 0, 0, 0)}

	f, err := parseFillMessages(nil, nil, &datasetMessages{fillNew: newMsg, fillOld: oldMsg}, dt)
	require.NoError(t, err)
	require.Equal(t, []byte{1, 0, 0, 0}, f.Value)
	require.Equal(t, FillTimeAlloc, f.FillTime)

	f, err = parseFillMessages(nil, nil, &datasetMessages{fillOld: oldMsg}, dt)
	require.NoError(t, err)
	require.Equal(t, []byte{2, 0, 0, 0}, f.Value)
	require.Equal(t, AllocTimeLate, f.AllocTime)

	f, err = parseFillMessages(nil, nil, &datasetMessages{}, dt)
	require.NoError(t, err)
	require.Nil(t, f)
}

// TestChunksMissing counts grid cells against present chunks.
func TestChunksMissing(t *testing.T) {
	d := &DatasetDescriptor{
		dataspace: &DataspaceMessage{Dimensions: []uint64{10, 10}},
		layout:    &DataLayoutMessage{Class: LayoutChunked, ChunkSize: []uint64{4, 4, 4}},
	}
	require.True(t, d.ChunksMissing(6))
	require.False(t, d.ChunksMissing(9))

	huge := &DatasetDescriptor{
		dataspace: &DataspaceMessage{Dimensions: []uint64{1 << 40, 1 << 40}},
		layout:    &DataLayoutMessage{Class: LayoutChunked, ChunkSize: []uint64{1, 1, 4}},
	}
	require.True(t, huge.ChunksMissing(1<<20), "an overflowing grid counts as missing chunks")
}

// FuzzParseFillValueMessage: neither fill value decoder may panic, and an
// accepted message must hold exactly the value bytes its size claims.
func FuzzParseFillValueMessage(f *testing.F) {
	f.Add([]byte{1, AllocTimeLate, FillTimeIfSet, 0}, uint32(4))
	f.Add(append(le32([]byte{2, AllocTimeEarly, FillTimeIfSet, 1}, 4), 99, 0, 0, 0), uint32(4))
	f.Add(append(le32([]byte{3, 0x20}, 2), 0xAB, 0xCD), uint32(2))
	f.Add(le32([]byte{3, 0x20}, 0xFFFFFFFF), uint32(8))
	f.Add(append(le32(nil, 4), 1, 2, 3, 4), uint32(4))

	f.Fuzz(func(t *testing.T, data []byte, dtSize uint32) {
		for _, parse := range []func() (*FillValueMessage, error){
			func() (*FillValueMessage, error) { return ParseFillValueMessage(data) },
			func() (*FillValueMessage, error) { return ParseFillValueOldMessage(data, dtSize) },
		} {
			fill, err := parse()
			if err != nil {
				continue
			}
			if fill.Size > 0 && int64(len(fill.Value)) != fill.Size {
				t.Fatalf("size %d with %d value bytes", fill.Size, len(fill.Value))
			}
			if int64(len(fill.Value)) > int64(len(data)) {
				t.Fatalf("value of %d bytes from a %d-byte message", len(fill.Value), len(data))
			}
		}
	})
}
