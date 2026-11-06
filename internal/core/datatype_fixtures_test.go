package core

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// Datatype fixtures for reader tests. Reader tests used to hand-build
// datatype bytes (an 8-byte header without properties, version 0, a v3
// compound with the member count in the properties) that the C library
// rejects; the decoder rejects them too since the codec (ADR-004 §3). These
// helpers produce the bytes with the codec constructors instead, so a fixture
// is always a datatype the C library reads.

// fixtureDatatype returns a little-endian datatype of the given class and
// size: a signed integer, an IEEE float or a null-terminated ASCII string.
func fixtureDatatype(class DatatypeClass, size uint32) *DatatypeMessage {
	var dt *DatatypeMessage
	var err error
	switch class {
	case DatatypeFixed:
		dt, err = NewIntegerDatatype(size, true)
	case DatatypeFloat:
		dt, err = NewFloatDatatype(size)
	case DatatypeString:
		dt, err = NewStringDatatype(size, StringNullTerm, CharsetASCII)
	default:
		err = fmt.Errorf("fixtureDatatype: unsupported class %d", class)
	}
	if err != nil {
		panic(err)
	}
	return dt
}

// fixtureBytes encodes dt; it panics on error, which fails the calling test.
func fixtureBytes(dt *DatatypeMessage) []byte {
	b, err := EncodeDatatypeMessage(dt)
	if err != nil {
		panic(err)
	}
	return b
}

// fixtureDatatypeBytes is fixtureDatatype, encoded.
func fixtureDatatypeBytes(class DatatypeClass, size uint32) []byte {
	return fixtureBytes(fixtureDatatype(class, size))
}

// fixtureCompoundBytes encodes a version 3 compound of size bytes with the
// given members (class and size per member as in fixtureDatatype).
func fixtureCompoundBytes(size uint32, members []testCompoundMember) []byte {
	ms := make([]CompoundMember, len(members))
	for i, m := range members {
		ms[i] = CompoundMember{Name: m.name, Offset: m.offset, Type: fixtureDatatype(m.dtClass, m.dtSize)}
	}
	dt, err := NewCompoundDatatype(size, ms)
	if err != nil {
		panic(err)
	}
	return fixtureBytes(dt)
}

// fixtureVLenBytes encodes a variable-length sequence of unsigned bytes whose
// element (heap reference) size is size.
func fixtureVLenBytes(size uint32) []byte {
	base, err := NewIntegerDatatype(1, false)
	if err != nil {
		panic(err)
	}
	return fixtureBytes(&DatatypeMessage{Class: DatatypeVarLen, Version: 1, Size: size, Base: base})
}

// fixtureUnsignedByte is H5T_NATIVE_UCHAR, the base of a C variable-length
// string.
func fixtureUnsignedByte() *DatatypeMessage {
	dt, err := NewIntegerDatatype(1, false)
	if err != nil {
		panic(err)
	}
	return dt
}

// TestDatatypeFixtures_Valid pins the fixtures to complete, C-readable
// datatype messages: each decodes, consumes all its bytes and re-encodes to
// itself.
func TestDatatypeFixtures_Valid(t *testing.T) {
	fixtures := map[string][]byte{
		"int32":    fixtureDatatypeBytes(DatatypeFixed, 4),
		"float64":  fixtureDatatypeBytes(DatatypeFloat, 8),
		"string16": fixtureDatatypeBytes(DatatypeString, 16),
		"compound": fixtureCompoundBytes(12, []testCompoundMember{
			{name: "a", offset: 0, dtClass: DatatypeFixed, dtSize: 4},
			{name: "b", offset: 4, dtClass: DatatypeFloat, dtSize: 8},
		}),
		"vlen":  fixtureVLenBytes(16),
		"uchar": fixtureBytes(fixtureUnsignedByte()),
	}
	for name, b := range fixtures {
		dt, n, err := DecodeDatatype(b)
		require.NoError(t, err, name)
		require.Equal(t, len(b), n, name)
		again, err := EncodeDatatypeMessage(dt)
		require.NoError(t, err, name)
		require.Equal(t, b, again, name)
	}
	require.Equal(t, int32LEBytes, fixtureDatatypeBytes(DatatypeFixed, 4))
}
