package core

import (
	"encoding/binary"
	"testing"

	"github.com/scigolib/hdf5/internal/utils"
	"github.com/stretchr/testify/require"
)

// TestEncodeExternalLinkValue_CLayout: the bytes H5Lcreate_external builds
// (H5L.c:644-656) behind the 16-bit length H5O__link_encode writes
// (H5Olink.c:384-391).
func TestEncodeExternalLinkValue_CLayout(t *testing.T) {
	v, err := EncodeExternalLinkValue("o.h5", "/x")
	require.NoError(t, err)
	require.Equal(t, []byte{9, 0, 0x00, 'o', '.', 'h', '5', 0, '/', 'x', 0}, v)

	ext, err := DecodeExternalLinkValue(v)
	require.NoError(t, err)
	require.Equal(t, ExternalLinkValue{FileName: "o.h5", ObjectPath: "/x", Size: len(v)}, ext)

	// Padding after the value (a version 1 header aligns messages) is not part of it.
	ext, err = DecodeExternalLinkValue(append(v, 0, 0, 0))
	require.NoError(t, err)
	require.Equal(t, len(v), ext.Size)

	_, err = EncodeExternalLinkValue("a\x00b", "/x")
	require.Error(t, err)
}

// mustExternalLinkValue is EncodeExternalLinkValue for valid test names.
func mustExternalLinkValue(t *testing.T, fileName, objectPath string) []byte {
	t.Helper()
	v, err := EncodeExternalLinkValue(fileName, objectPath)
	require.NoError(t, err)
	return v
}

// legacyExternalLinkValue is the value hdf5-go v0.14.2 and earlier wrote:
// file name length, file name, path length, path.
func legacyExternalLinkValue(file, path string) []byte {
	v := binary.LittleEndian.AppendUint16(nil, uint16(len(file)))
	v = append(v, file...)
	v = binary.LittleEndian.AppendUint16(v, uint16(len(path)))
	return append(v, path...)
}

func TestDecodeExternalLinkValue_Legacy(t *testing.T) {
	v := legacyExternalLinkValue("other.h5", "/x")
	ext, err := DecodeExternalLinkValue(v)
	require.NoError(t, err)
	require.Equal(t, ExternalLinkValue{FileName: "other.h5", ObjectPath: "/x", Size: len(v), Legacy: true}, ext)

	lm := &LinkMessage{Version: 1, Flags: LinkFlagLinkTypeFieldBit, Type: LinkTypeExternal, Name: "ext", LinkValue: v}
	data, err := EncodeLinkMessage(lm, nil)
	require.NoError(t, err)
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}
	got, err := ParseLinkMessageAt(data, sb, 0x400)
	require.NoError(t, err)
	file, path, err := got.GetExternalLinkInfo()
	require.NoError(t, err)
	require.Equal(t, []string{"other.h5", "/x"}, []string{file, path})
	require.True(t, sb.HasWarning(WarnLegacyExternalLink))
	require.Equal(t, uint64(0x400), sb.Warnings()[0].Addr)

	// The C layout records nothing.
	cv, err := EncodeExternalLinkValue("other.h5", "/x")
	require.NoError(t, err)
	lm.LinkValue = cv
	data, err = EncodeLinkMessage(lm, nil)
	require.NoError(t, err)
	sb2 := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}
	_, err = ParseLinkMessageAt(data, sb2, 0x400)
	require.NoError(t, err)
	require.False(t, sb2.HasWarning(WarnLegacyExternalLink))
}

func TestDecodeExternalLinkValue_Corrupt(t *testing.T) {
	for name, v := range map[string][]byte{
		"short":              {1},
		"bad version":        {4, 0, 0x10, 'a', 0, 0},
		"bad flags":          {4, 0, 0x01, 'a', 0, 0},
		"one string":         {3, 0, 0x00, 'a', 0},
		"unterminated path":  {5, 0, 0x00, 'a', 0, 'b', 'c'},
		"three strings":      {6, 0, 0x00, 'a', 0, 'b', 0, 0},
		"legacy too short":   {3, 0, 'a', 'b', 'c', 5, 0, 'x'},
		"legacy extra bytes": append(legacyExternalLinkValue("f", "/p"), 'z'),
	} {
		_, err := DecodeExternalLinkValue(v)
		require.ErrorIs(t, err, utils.ErrCorrupt, name)
	}
}
