package core

import (
	"bytes"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"github.com/scigolib/hdf5/internal/utils"
)

// TestSharedLimitFailureRetried checks that a resolution refused by the budget
// is cached only while the budget stays the same: raising the file's read
// limit lets the same reference resolve.
func TestSharedLimitFailureRetried(t *testing.T) {
	const target = 0x100
	hdr := v1HeaderWith(HeaderMessage{Type: MsgDatatype, Flags: 0x01, Data: bigCompound(t, 100)})
	file := make([]byte, target+len(hdr))
	copy(file[target:], hdr)
	r := bytes.NewReader(file)
	ref := cat([]byte{sharedMsgVersion3, shareTypeCommitted}, le64(target))

	limit := uint64(64)
	sb := sharedTestSB()
	sb.SetSharedLimit(func() uint64 { return limit })

	_, err := resolveSharedDatatype(r, sb, ref)
	require.ErrorIs(t, err, utils.ErrLimit)
	_, err = resolveSharedDatatype(r, sb, ref)
	require.ErrorIs(t, err, utils.ErrLimit)

	limit = 1 << 20
	dt, err := resolveSharedDatatype(r, sb, ref)
	require.NoError(t, err)
	require.Len(t, dt.Members, 100)
}

// TestDatatypeNodeCostCoversStruct keeps datatypeNodeCost an upper bound of
// the structs it stands for.
func TestDatatypeNodeCostCoversStruct(t *testing.T) {
	require.LessOrEqual(t, unsafe.Sizeof(DatatypeMessage{}), uintptr(datatypeNodeCost))
	require.LessOrEqual(t, unsafe.Sizeof(CompoundMember{}), uintptr(datatypeNodeCost))
}
