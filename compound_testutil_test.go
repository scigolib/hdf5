package hdf5

import (
	"encoding/binary"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/require"
)

// testCompoundField describes one member of a compound datatype built in tests.
type testCompoundField struct {
	name   string
	offset uint32
	typ    *core.DatatypeMessage
}

// testBasicType builds a basic member datatype (fixed, float, or string) for
// compound construction in tests.
func testBasicType(class core.DatatypeClass, size uint32) *core.DatatypeMessage {
	var props []byte
	switch class {
	case core.DatatypeFixed:
		props = []byte{0, byte(size * 8), 0, 0}
	case core.DatatypeFloat:
		props = make([]byte, 12)
		props[1] = byte(size * 8)
	case core.DatatypeString:
		props = []byte{0}
	}
	return &core.DatatypeMessage{Class: class, Version: 1, Size: size, Properties: props}
}

// testEncodeCompoundV3 encodes a version-3 compound datatype message
// (header + member definitions) from the given fields.
func testEncodeCompoundV3(totalSize uint32, fields []testCompoundField) []byte {
	le := binary.LittleEndian
	buf := make([]byte, 0, 64)
	buf = le.AppendUint32(buf, uint32(core.DatatypeCompound)|3<<4)
	buf = le.AppendUint32(buf, totalSize)
	buf = le.AppendUint32(buf, uint32(len(fields)))
	for _, f := range fields {
		buf = append(buf, f.name...)
		buf = append(buf, 0)
		buf = le.AppendUint32(buf, f.offset)
		buf = le.AppendUint32(buf, uint32(f.typ.Class)|uint32(f.typ.Version)<<4|f.typ.ClassBitField<<8)
		buf = le.AppendUint32(buf, f.typ.Size)
		buf = append(buf, f.typ.Properties...)
	}
	return buf
}

// testCompoundType builds a compound DatatypeMessage from fields by encoding
// a version-3 compound message and parsing it back. Field offsets must be
// contiguous; the total size is the sum of member sizes.
func testCompoundType(t *testing.T, fields []testCompoundField) *core.DatatypeMessage {
	t.Helper()
	var totalSize uint32
	for _, f := range fields {
		totalSize += f.typ.Size
	}
	dt, err := core.ParseDatatypeMessage(testEncodeCompoundV3(totalSize, fields))
	require.NoError(t, err)
	return dt
}
