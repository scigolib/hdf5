package core

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// udLinkMessage encodes a version 1 link message of type linkType named
// name whose value is the 2-byte length and udata, as H5O__link_encode
// writes a user-defined link.
func udLinkMessage(linkType uint8, name string, udata []byte) []byte {
	msg := []byte{1, LinkFlagLinkTypeFieldBit, linkType, byte(len(name))}
	msg = append(msg, name...)
	msg = binary.LittleEndian.AppendUint16(msg, uint16(len(udata)))
	return append(msg, udata...)
}

// TestParseLinkMessage_UserDefined: link types 64 to 255 are user-defined
// links, whose value is a 2-byte length and that many bytes
// (H5O__link_decode, H5Olink.c:239-262); types 2 to 63 are unknown. Red
// before the change: every type but 0, 1 and 64 was refused, so the write
// path refused a valid file with a user-defined link (hdf5_official
// twithub.h5, /g2/udlink, type 187).
func TestParseLinkMessage_UserDefined(t *testing.T) {
	sb := &Superblock{OffsetSize: 8, Endianness: binary.LittleEndian}

	lm, err := ParseLinkMessage(udLinkMessage(187, "udlink", nil), sb)
	require.NoError(t, err)
	require.Equal(t, LinkType(187), lm.Type)
	require.Equal(t, "udlink", lm.Name)
	require.Equal(t, []byte{0, 0}, lm.LinkValue)

	raw := udLinkMessage(255, "u", []byte{1, 2, 3})
	lm, err = ParseLinkMessage(raw, sb)
	require.NoError(t, err)
	require.Equal(t, []byte{3, 0, 1, 2, 3}, lm.LinkValue)
	again, err := EncodeLinkMessage(lm, sb)
	require.NoError(t, err)
	require.Equal(t, raw, again, "a user-defined link re-encodes unchanged")

	_, err = ParseLinkMessage(udLinkMessage(200, "u", []byte{1, 2, 3})[:9], sb)
	require.Error(t, err, "user data past the end of the message")

	_, err = ParseLinkMessage(udLinkMessage(5, "u", nil), sb)
	require.Error(t, err, "types 2 to 63 are unknown")
}
