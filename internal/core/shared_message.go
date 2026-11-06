package core

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sync"

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
// Each target is read once per file: the result is cached in the superblock
// (sharedCache), as C keeps the target header in its metadata cache
// (H5O_msg_read → H5O_protect). The returned body is shared; callers must not
// modify it.
func resolveSharedMessage(r io.ReaderAt, sb *Superblock, data []byte, msgType MessageType) ([]byte, error) {
	addr, err := committedTarget(r, sb, data, msgType)
	if err != nil {
		return nil, err
	}

	sb.shared.mu.Lock()
	defer sb.shared.mu.Unlock()

	return sb.shared.bodyLocked(r, sb, addr, msgType)
}

// resolveSharedDatatype returns the committed datatype a shared datatype
// reference points at, decoded once per file and shared read-only by every
// attribute and dataset that refers to it. Without the sharing, each 37-byte
// attribute reference to a large compound type would decode its own copy of
// the type (review-068 finding 2: ~640 KiB per attribute).
func resolveSharedDatatype(r io.ReaderAt, sb *Superblock, data []byte) (*DatatypeMessage, error) {
	addr, err := committedTarget(r, sb, data, MsgDatatype)
	if err != nil {
		return nil, err
	}

	c := &sb.shared
	c.mu.Lock()
	defer c.mu.Unlock()

	if dt, ok := c.datatypes[addr]; ok {
		return dt, nil
	}

	body, err := c.bodyLocked(r, sb, addr, MsgDatatype)
	if err != nil {
		return nil, err
	}

	dt, err := ParseDatatypeMessage(body)
	if err != nil {
		return nil, fmt.Errorf("committed datatype in object header 0x%X: %w", addr, err)
	}

	if c.datatypes == nil {
		c.datatypes = make(map[uint64]*DatatypeMessage)
	}
	c.datatypes[addr] = dt

	return dt, nil
}

// committedTarget decodes a shared-message reference and returns the address
// of the object header that holds the committed message.
func committedTarget(r io.ReaderAt, sb *Superblock, data []byte, msgType MessageType) (uint64, error) {
	if r == nil || sb == nil {
		return 0, fmt.Errorf("shared message of type %d: no file to resolve it in", msgType)
	}

	sm, err := decodeSharedMessage(data, sb)
	if err != nil {
		return 0, err
	}

	if sm.kind == shareTypeSOHM {
		return 0, fmt.Errorf("message of type %d: %w", msgType, errSOHMNotSupported)
	}

	return sm.address, nil
}

// sharedResolveBudget is the most object header bytes one file may read to
// resolve shared messages. Every distinct target header is read once and
// counted once, so a valid file stays far below it: a committed datatype's
// header holds one message of at most H5O_MESG_MAX_SIZE (64 KiB,
// H5Oprivate.h:67) and a few small ones, and 16 MiB covers hundreds of
// thousands of committed datatypes. A crafted file can point thousands of
// 30-byte headers at one large continuation chunk, or thousands of attributes
// at distinct large types; without a budget each costs a full header parse and
// a decoded type kept for the life of the File (review-068 finding 2). The
// budget bounds the bytes parsed, the bodies cached and, through them, the
// decoded datatypes. Exceeding it fails the read with utils.ErrLimit.
//
// Deviation: C has no such bound; its metadata cache evicts headers instead
// (H5AC). A Go program cannot recover from a failed allocation (ADR-003).
const sharedResolveBudget uint64 = 16 << 20

// sharedKey names one committed message: its object header and message type.
type sharedKey struct {
	address uint64
	msgType MessageType
}

// sharedCache holds the committed messages resolved in one file, keyed by
// target header and message type, and the datatypes decoded from them. Entries
// are immutable once stored and shared by every reader of the file. Its zero
// value is ready to use; it is safe for concurrent use.
//
// The library never writes shared messages, so a write session cannot change
// a cached target in place. A deleted committed datatype whose space the
// allocator reuses would leave a stale entry, read only through a reference
// that already dangles.
type sharedCache struct {
	mu        sync.Mutex
	bodies    map[sharedKey][]byte
	datatypes map[uint64]*DatatypeMessage
	spent     uint64 // Target header bytes read so far, against budget.
	budget    uint64 // 0 means sharedResolveBudget; tests lower it.
}

// bodyLocked returns the body of the first message of type msgType in the
// object header at addr, reading the header on first use. c.mu must be held.
func (c *sharedCache) bodyLocked(r io.ReaderAt, sb *Superblock, addr uint64, msgType MessageType) ([]byte, error) {
	key := sharedKey{address: addr, msgType: msgType}
	if body, ok := c.bodies[key]; ok {
		return body, nil
	}

	header, err := readObjectHeader(r, addr, sb, false)
	if err != nil {
		return nil, sharedTargetError(msgType, addr, err)
	}

	if err := c.charge(header, addr); err != nil {
		return nil, err
	}

	for _, msg := range header.Messages {
		if msg.Type != msgType {
			continue
		}

		if msg.Flags&MsgFlagShared != 0 {
			return nil, fmt.Errorf("%w: committed message of type %d in object header 0x%X is itself shared",
				utils.ErrCorrupt, msgType, addr)
		}

		// A copy: msg.Data may share the buffer of the whole header chunk,
		// which the cache must not keep alive.
		body := bytes.Clone(msg.Data)
		if c.bodies == nil {
			c.bodies = make(map[sharedKey][]byte)
		}
		c.bodies[key] = body

		return body, nil
	}

	return nil, fmt.Errorf("%w: object header 0x%X has no message of type %d for the shared message referring to it",
		utils.ErrCorrupt, addr, msgType)
}

// charge counts the bytes of a target header just read against the file's
// resolution budget: each message's body and its 8-byte message header.
func (c *sharedCache) charge(header *ObjectHeader, addr uint64) error {
	budget := c.budget
	if budget == 0 {
		budget = sharedResolveBudget
	}

	cost := uint64(0)
	for _, msg := range header.Messages {
		cost += uint64(len(msg.Data)) + sharedMsgHeaderCost
	}

	if cost > budget || c.spent > budget-cost {
		return fmt.Errorf("%w: resolving the shared message in object header 0x%X would read %d bytes "+
			"after %d already read for shared messages, more than the %d bytes allowed per file",
			utils.ErrLimit, addr, cost, c.spent, budget)
	}
	c.spent += cost

	return nil
}

// sharedMsgHeaderCost is the cost charged per message beyond its body: the
// version 1 message header size (H5O_SIZEOF_MSGHDR_VERS, H5Opkg.h:112-117).
const sharedMsgHeaderCost = 8

// sharedTargetError classifies a failure to read the target header of a shared
// message, as filterError does for filters: a limit or an unsupported feature
// keeps its own sentinel, anything else is corruption of the reference.
func sharedTargetError(msgType MessageType, addr uint64, err error) error {
	if errors.Is(err, utils.ErrCorrupt) || errors.Is(err, utils.ErrLimit) || errors.Is(err, utils.ErrNotSupported) {
		return fmt.Errorf("shared message of type %d refers to object header 0x%X: %w", msgType, addr, err)
	}

	return fmt.Errorf("%w: shared message of type %d refers to object header 0x%X: %w",
		utils.ErrCorrupt, msgType, addr, err)
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
