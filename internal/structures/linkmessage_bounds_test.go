package structures

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/require"
)

// TestParseLinkMessage_NameLengthBounds covers the file-derived link name
// length: the 8-byte encoding can claim a size that does not fit in int, and
// converting it first turns negative, which makes the "name extends beyond the
// message" check pass and the slice below panic. The value must be rejected
// before the conversion, on every platform.
func TestParseLinkMessage_NameLengthBounds(t *testing.T) {
	sb := &core.Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	tests := []struct {
		name    string
		length  uint64
		wantErr bool
	}{
		{name: "empty name is rejected", length: 0, wantErr: true},
		{name: "size above platform int", length: math.MaxUint64, wantErr: true},
		{name: "valid short name", length: 3, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 10 bytes of header plus room for a three-byte name and the
			// eight-byte hard link address.
			data := make([]byte, 21)
			data[0] = 1    // version
			data[1] = 0x03 // link name length encoded in 8 bytes
			binary.LittleEndian.PutUint64(data[2:10], tt.length)
			if !tt.wantErr {
				copy(data[10:], "abc")
			}

			msg, err := ParseLinkMessage(data, sb)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, "abc", msg.Name)
		})
	}
}
