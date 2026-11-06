package core

import (
	"errors"
	"fmt"
	"io"

	"github.com/scigolib/hdf5/internal/utils"
)

// Shared message versions (H5O_SHARED_VERSION_1..3, H5Oshared.c:46-53), decoded by
// H5O__shared_decode (H5Oshared.c:289).
const (
	sharedMsgVersion1 uint8 = 1
	sharedMsgVersion2 uint8 = 2
	sharedMsgVersion3 uint8 = 3
)

// Where a shared message is stored (H5Oprivate.h:251-254). Only the two stored
// kinds can appear in a shared-message reference (H5O_IS_STORED_SHARED,
// H5Oprivate.h:258).
const (
	shareTypeSOHM      uint8 = 1 // H5O_SHARE_TYPE_SOHM: the shared message heap.
	shareTypeCommitted uint8 = 2 // H5O_SHARE_TYPE_COMMITTED: another object header.
)

// sharedMsgHeapIDSize is sizeof(H5O_fheap_id_t), the heap ID of a message in
// the shared object header message heap (H5O_FHEAP_ID_LEN, H5Oprivate.h:64).
const sharedMsgHeapIDSize = 8

// errSOHMNotSupported reports a message shared through the file-wide shared
// object header message heap (SOHM, superblock extension message 0x000F).
// Such files are valid HDF5 that this library cannot resolve yet, so the error
// wraps utils.ErrNotSupported, not utils.ErrCorrupt.
var errSOHMNotSupported = fmt.Errorf("message shared in the shared object header message heap (SOHM): %w", utils.ErrNotSupported)

// IsSOHMNotSupported reports whether err says that a message is stored in the
// shared object header message heap, which the library cannot read yet.
func IsSOHMNotSupported(err error) bool {
	return errors.Is(err, errSOHMNotSupported)
}

// sharedMessage is a decoded shared-message reference (H5O_shared_t).
type sharedMessage struct {
	version uint8
	kind    uint8  // shareTypeSOHM or shareTypeCommitted.
	address uint64 // Object header address of a committed message.
}

// decodeSharedMessage decodes the body of a message whose header flags carry
// H5O_MSG_FLAG_SHARED, as H5O__shared_decode does (H5Oshared.c:289-367):
//
//	version 1: version, reserved type byte, 6 reserved bytes, a length-sized
//	           field C skips (H5Oshared.c:318), the object header address;
//	version 2: version, type byte, the object header address (always a
//	           committed message, H5Oshared.c:340-341);
//	version 3: version, type, then either an 8-byte SOHM heap ID or the
//	           object header address.
//
// Deviations, all stricter: C asserts that a version-2 message is never in
// the SOHM heap (H5Oshared.c:330) and that the type of a version-3 message is
// SOHM or committed (H5Oshared.c:107, :148); release builds of C go on with
// garbage there, this decoder reports utils.ErrCorrupt. An undefined object
// header address is corrupt too: H5O_msg_read could not load a header there.
func decodeSharedMessage(data []byte, sb *Superblock) (*sharedMessage, error) {
	if len(data) < 2 {
		return nil, fmt.Errorf("%w: shared message reference of %d bytes is too short", utils.ErrCorrupt, len(data))
	}

	sm := &sharedMessage{version: data[0], kind: shareTypeCommitted}
	if sm.version < sharedMsgVersion1 || sm.version > sharedMsgVersion3 {
		return nil, fmt.Errorf("%w: bad version number %d for shared object message (H5Oshared.c:308)",
			utils.ErrCorrupt, sm.version)
	}

	offset := 2 // Version and type (reserved in version 1, H5Oshared.c:317).
	switch sm.version {
	case sharedMsgVersion1:
		offset += 6 + int(sb.LengthSize)
	case sharedMsgVersion2:
		if data[1] == shareTypeSOHM {
			return nil, fmt.Errorf("%w: version 2 shared message refers to the shared message heap", utils.ErrCorrupt)
		}
	case sharedMsgVersion3:
		sm.kind = data[1]
		switch sm.kind {
		case shareTypeSOHM:
			if len(data) < offset+sharedMsgHeapIDSize {
				return nil, fmt.Errorf("%w: shared message heap ID runs off the end of the message", utils.ErrCorrupt)
			}
			return sm, nil
		case shareTypeCommitted:
		default:
			return nil, fmt.Errorf("%w: shared message type %d is neither heap (1) nor committed (2)",
				utils.ErrCorrupt, sm.kind)
		}
	}

	addrSize := int(sb.OffsetSize)
	if len(data) < offset+addrSize {
		return nil, fmt.Errorf("%w: shared message version %d needs %d bytes, has %d",
			utils.ErrCorrupt, sm.version, offset+addrSize, len(data))
	}

	sm.address = readLayoutAddress(data[offset:offset+addrSize], addrSize, sb.Endianness)
	if sm.address == undefinedAddress {
		return nil, fmt.Errorf("%w: shared message refers to an undefined object header address", utils.ErrCorrupt)
	}

	return sm, nil
}

// resolveSharedMessage returns the body of the message of type msgType that the
// shared-message reference data points at, as H5O__shared_read does
// (H5Oshared.c:95-189) for a committed message: it loads the object header at
// the reference's address and reads its first message of that type
// (H5O_msg_read, H5O_msg_read_oh).
//
// Resolution is one level deep and reads the target header without its
// attributes, so no file can make it recurse:
//   - C never writes a committed message that is itself shared (a committed
//     datatype is stored in its own header, H5T__commit), so a target message
//     with H5O_MSG_FLAG_SHARED, including a message that points at itself, is
//     utils.ErrCorrupt. C would recurse on such a chain; a crafted cycle would
//     exhaust its stack;
//   - attributes are not parsed while the target header is read, so an
//     attribute whose datatype is the committed type it is attached to (valid
//     HDF5, tnamed_dtype_attr.h5) does not start another resolution.
//
// Nothing is cached: each resolution reads one object header, bounded like
// every other header read.
func resolveSharedMessage(r io.ReaderAt, sb *Superblock, data []byte, msgType MessageType) ([]byte, error) {
	if r == nil || sb == nil {
		return nil, fmt.Errorf("shared message of type %d: no file to resolve it in", msgType)
	}

	sm, err := decodeSharedMessage(data, sb)
	if err != nil {
		return nil, err
	}

	if sm.kind == shareTypeSOHM {
		return nil, fmt.Errorf("message of type %d: %w", msgType, errSOHMNotSupported)
	}

	header, err := readObjectHeader(r, sm.address, sb, false)
	if err != nil {
		return nil, fmt.Errorf("%w: shared message of type %d refers to object header 0x%X: %w",
			utils.ErrCorrupt, msgType, sm.address, err)
	}

	for _, msg := range header.Messages {
		if msg.Type != msgType {
			continue
		}

		if msg.Flags&MsgFlagShared != 0 {
			return nil, fmt.Errorf("%w: committed message of type %d in object header 0x%X is itself shared",
				utils.ErrCorrupt, msgType, sm.address)
		}

		return msg.Data, nil
	}

	return nil, fmt.Errorf("%w: object header 0x%X has no message of type %d for the shared message referring to it",
		utils.ErrCorrupt, sm.address, msgType)
}

// messageBody returns the body of a header message, following a shared-message
// reference when the message carries H5O_MSG_FLAG_SHARED (the shared decode
// wrapper H5O_SHARED_DECODE, H5Oshared.h:59-63).
func messageBody(r io.ReaderAt, sb *Superblock, msg *HeaderMessage) ([]byte, error) {
	if msg.Flags&MsgFlagShared == 0 {
		return msg.Data, nil
	}

	body, err := resolveSharedMessage(r, sb, msg.Data, msg.Type)
	if err != nil {
		return nil, fmt.Errorf("shared message at 0x%x: %w", msg.Offset, err)
	}

	return body, nil
}
