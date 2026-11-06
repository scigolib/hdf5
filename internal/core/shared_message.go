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
	if err := cachedFailureLocked(c, c.badTypes, addr); err != nil {
		return nil, err
	}

	dt, err := c.decodeDatatypeLocked(r, sb, addr)
	if err != nil {
		c.badTypes = recordFailureLocked(c, c.badTypes, addr, err)

		return nil, err
	}

	if c.datatypes == nil {
		c.datatypes = make(map[uint64]*DatatypeMessage)
	}
	c.datatypes[addr] = dt

	return dt, nil
}

// datatypeNodeCost is the charge for one decoded DatatypeMessage or
// CompoundMember beyond the data its slices and strings hold: an upper bound
// of the struct sizes on 64-bit platforms (checked by
// TestDatatypeNodeCostCoversStruct).
const datatypeNodeCost = 256

// datatypeFootprint estimates the bytes a decoded datatype keeps alive: one
// node per type and member, plus its properties, tag, enumeration names and
// values, array dimensions, member names, member types and base type.
func datatypeFootprint(dt *DatatypeMessage) uint64 {
	if dt == nil {
		return 0
	}

	n := uint64(datatypeNodeCost) + uint64(len(dt.Properties)) + uint64(len(dt.OpaqueTag)) +
		uint64(len(dt.EnumValues)) + 4*uint64(len(dt.ArrayDims))
	for _, name := range dt.EnumNames {
		n += 16 + uint64(len(name)) // String header and bytes.
	}
	for i := range dt.Members {
		n += datatypeNodeCost + uint64(len(dt.Members[i].Name)) + datatypeFootprint(dt.Members[i].Type)
	}

	return n + datatypeFootprint(dt.Base)
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

// SetSharedLimit ties the budget of the shared message cache to fn, which
// returns the file's read limit (hdf5.File.ReadLimit); it is called on each
// resolution, so a changed limit applies to the next one. Without it, or when
// fn returns 0, the budget is utils.DefaultReadLimit.
//
// The budget bounds what one file keeps for the life of the File to resolve
// shared messages: the bodies of the committed messages (each with its 8-byte
// message header), an estimate of the datatypes decoded from them
// (datatypeFootprint) and the External File List heap blocks
// (cachedLocalHeapBlock). Every distinct target is charged once. A crafted file
// can point thousands of attributes at distinct large committed types; without
// a budget each one decoded stays cached (review-068 finding 2). Exceeding the
// budget fails the read with utils.ErrLimit; raising the limit lets it proceed.
//
// Deviation: C has no such bound; its metadata cache evicts headers instead
// (H5AC). A Go program cannot recover from a failed allocation (ADR-003).
func (sb *Superblock) SetSharedLimit(fn func() uint64) {
	sb.shared.mu.Lock()
	defer sb.shared.mu.Unlock()

	sb.shared.limit = fn
}

// sharedKey names one committed message: its object header and message type.
type sharedKey struct {
	address uint64
	msgType MessageType
}

// sharedCache holds the committed messages resolved in one file, keyed by
// target header and message type, the datatypes decoded from them, and the
// External File List heap blocks. Entries are immutable once stored and shared
// by every reader of the file. Its zero value is ready to use; it is safe for
// concurrent use.
//
// The library never writes shared messages or External File Lists, so a write
// session cannot change a cached target in place. A deleted committed datatype whose space the
// allocator reuses would leave a stale entry, read only through a reference
// that already dangles.
//
// Failures decided by the file's bytes are cached too (review-r finding 2): a
// corrupt or unsupported target fails the same way for every reference to it,
// so a file with thousands of references to one bad target parses it once. A
// failure caused by the budget is kept only while the budget is unchanged, so
// a raised read limit retries it. A failed read is not kept
// (recordFailureLocked). Deviation: C caches no failure; it re-reads the header
// through its metadata cache on every reference.
type sharedCache struct {
	mu        sync.Mutex
	bodies    map[sharedKey][]byte
	datatypes map[uint64]*DatatypeMessage
	failed    map[sharedKey]sharedFailure // Targets whose body could not be read.
	badTypes  map[uint64]sharedFailure    // Committed datatypes that could not be decoded.
	heaps     map[uint64][]byte           // External File List heap blocks (cachedLocalHeapBlock).
	badHeaps  map[uint64]sharedFailure    // Heaps that could not be read.
	spent     uint64                      // Bytes charged so far, against the budget.
	limit     func() uint64               // The file's read limit (SetSharedLimit); nil: the default.
}

// sharedFailure is a cached resolution failure. budget is the budget in effect
// when a failure wrapping utils.ErrLimit happened, 0 for any other failure.
type sharedFailure struct {
	err    error
	budget uint64
}

// budgetLocked returns the budget in effect. c.mu must be held.
func (c *sharedCache) budgetLocked() uint64 {
	if c.limit != nil {
		if limit := c.limit(); limit != 0 {
			return limit
		}
	}

	return utils.DefaultReadLimit
}

// recordFailureLocked caches err as the failure of key when the file's bytes
// decide it: an error wrapping utils.ErrCorrupt or utils.ErrNotSupported that
// is not a failed read (readError), or a budget refusal (utils.ErrLimit),
// kept with the budget in effect. A failed read and any other error are not
// cached, so a transient I/O error is retried on the next access. c.mu must be
// held. It returns failures, allocated on first use.
func recordFailureLocked[K comparable](c *sharedCache, failures map[K]sharedFailure, key K, err error) map[K]sharedFailure {
	f := sharedFailure{err: err}
	var rf *readError
	switch {
	case errors.Is(err, utils.ErrLimit):
		f.budget = c.budgetLocked()
	case errors.As(err, &rf):
		return failures
	case errors.Is(err, utils.ErrCorrupt), errors.Is(err, utils.ErrNotSupported):
	default:
		return failures
	}

	if failures == nil {
		failures = make(map[K]sharedFailure)
	}
	failures[key] = f

	return failures
}

// readError marks an error of the reader, not of the file's bytes, that
// sharedTargetError reports as corruption of the reference: it is not cached.
type readError struct{ err error }

func (e *readError) Error() string { return e.err.Error() }
func (e *readError) Unwrap() error { return e.err }

// cachedFailureLocked returns the cached failure of key, unless it was caused
// by a budget that has changed since; nil when there is none. c.mu must be
// held.
func cachedFailureLocked[K comparable](c *sharedCache, failures map[K]sharedFailure, key K) error {
	f, ok := failures[key]
	if !ok || (f.budget != 0 && f.budget != c.budgetLocked()) {
		return nil
	}

	return f.err
}

// bodyLocked returns the body of the first message of type msgType in the
// object header at addr, reading the header on first use. A failure is cached
// like a body. c.mu must be held.
func (c *sharedCache) bodyLocked(r io.ReaderAt, sb *Superblock, addr uint64, msgType MessageType) ([]byte, error) {
	key := sharedKey{address: addr, msgType: msgType}
	if body, ok := c.bodies[key]; ok {
		return body, nil
	}
	if err := cachedFailureLocked(c, c.failed, key); err != nil {
		return nil, err
	}

	body, err := c.readBodyLocked(r, sb, addr, msgType)
	if err != nil {
		c.failed = recordFailureLocked(c, c.failed, key, err)

		return nil, err
	}

	if c.bodies == nil {
		c.bodies = make(map[sharedKey][]byte)
	}
	c.bodies[key] = body

	return body, nil
}

// readBodyLocked reads the object header at addr and returns a copy of its
// first message of type msgType, charged to the budget. c.mu must be held.
func (c *sharedCache) readBodyLocked(r io.ReaderAt, sb *Superblock, addr uint64, msgType MessageType) ([]byte, error) {
	header, err := readObjectHeader(r, addr, sb, false)
	if err != nil {
		return nil, sharedTargetError(msgType, addr, err)
	}

	for _, msg := range header.Messages {
		if msg.Type != msgType {
			continue
		}

		if msg.Flags&MsgFlagShared != 0 {
			return nil, fmt.Errorf("%w: committed message of type %d in object header 0x%X is itself shared",
				utils.ErrCorrupt, msgType, addr)
		}

		// Only the message kept is charged: the rest of the header is parsed
		// and dropped.
		if err := c.chargeLocked(uint64(len(msg.Data))+sharedMsgHeaderCost, committedWhat(addr)); err != nil {
			return nil, err
		}

		// A copy: msg.Data may share the buffer of the whole header chunk,
		// which the cache must not keep alive.
		return bytes.Clone(msg.Data), nil
	}

	return nil, fmt.Errorf("%w: object header 0x%X has no message of type %d for the shared message referring to it",
		utils.ErrCorrupt, addr, msgType)
}

// chargeLocked counts cost bytes kept for what (as an error message names it)
// against the file's budget (SetSharedLimit). c.mu must be held.
func (c *sharedCache) chargeLocked(cost uint64, what string) error {
	budget := c.budgetLocked()
	if cost > budget || c.spent > budget-cost {
		return fmt.Errorf("%w: caching %s would keep %d more bytes after %d already kept "+
			"for shared metadata, more than the file's read limit of %d bytes",
			utils.ErrLimit, what, cost, c.spent, budget)
	}
	c.spent += cost

	return nil
}

// committedWhat names a committed message for chargeLocked.
func committedWhat(addr uint64) string {
	return fmt.Sprintf("the shared message in object header 0x%X", addr)
}

// sharedMsgHeaderCost is the cost charged per message beyond its body: the
// version 1 message header size (H5O_SIZEOF_MSGHDR_VERS, H5Opkg.h:112-117).
const sharedMsgHeaderCost = 8

// sharedTargetError classifies a failure to read the target header of a shared
// message, as filterError does for filters: a limit or an unsupported feature
// keeps its own sentinel, anything else is corruption of the reference. That
// includes a failed read, which is marked (readError) so the cache does not
// keep it.
func sharedTargetError(msgType MessageType, addr uint64, err error) error {
	if errors.Is(err, utils.ErrCorrupt) || errors.Is(err, utils.ErrLimit) || errors.Is(err, utils.ErrNotSupported) {
		return fmt.Errorf("shared message of type %d refers to object header 0x%X: %w", msgType, addr, err)
	}

	return fmt.Errorf("%w: shared message of type %d refers to object header 0x%X: %w",
		utils.ErrCorrupt, msgType, addr, &readError{err: err})
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

// decodeDatatypeLocked reads and decodes the committed datatype in the object
// header at addr and charges its decoded size to the budget. c.mu must be held.
func (c *sharedCache) decodeDatatypeLocked(r io.ReaderAt, sb *Superblock, addr uint64) (*DatatypeMessage, error) {
	body, err := c.bodyLocked(r, sb, addr, MsgDatatype)
	if err != nil {
		return nil, err
	}

	dt, err := ParseDatatypeMessage(body)
	if err != nil {
		return nil, fmt.Errorf("committed datatype in object header 0x%X: %w", addr, err)
	}

	if err := c.chargeLocked(datatypeFootprint(dt), committedWhat(addr)); err != nil {
		return nil, err
	}

	return dt, nil
}
