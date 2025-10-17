package core

import (
	"io"

	"github.com/scigolib/hdf5/internal/utils"
)

// parseV1Header parses a version 1 object header.
// V1 format (no "OHDR" signature):
// - Byte 0: Version (1).
// - Byte 1: Reserved (0).
// - Bytes 2-3: Number of header messages (uint16).
// - Bytes 4-7: Object reference count (uint32).
// - Bytes 8-11: Object header size (uint32).
// - Bytes 12-15: Padding to 8-byte boundary.
// - Then messages follow.
//
// Each message:
// - Bytes 0-1: Message type (uint16).
// - Bytes 2-3: Message data size (uint16).
// - Bytes 4: Message flags (uint8).
// - Bytes 5-7: Reserved (3 bytes).
// - Then message data.
func parseV1Header(r io.ReaderAt, headerAddr uint64, sb *Superblock) ([]*HeaderMessage, string, error) {
	// Read the header prefix (16 bytes).
	headerBuf := utils.GetBuffer(16)
	defer utils.ReleaseBuffer(headerBuf)

	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
	if _, err := r.ReadAt(headerBuf, int64(headerAddr)); err != nil {
		return nil, "", utils.WrapError("v1 header read failed", err)
	}

	// Parse header fields.
	version := headerBuf[0]
	if version != 1 {
		return nil, "", utils.WrapError("invalid v1 header version", nil)
	}

	numMessages := sb.Endianness.Uint16(headerBuf[2:4])
	//nolint:gocritic // commentedOutCode: valid comment explaining unused field
	// refCount := sb.Endianness.Uint32(headerBuf[4:8])  // Unused.
	headerSize := sb.Endianness.Uint32(headerBuf[8:12])

	// Messages start after the 16-byte header.
	current := headerAddr + 16
	end := headerAddr + uint64(headerSize)

	var messages []*HeaderMessage
	var name string

	for i := uint16(0); i < numMessages && current < end; i++ {
		// Read message header (8 bytes).
		msgHeaderBuf := utils.GetBuffer(8)
		//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
		if _, err := r.ReadAt(msgHeaderBuf, int64(current)); err != nil {
			utils.ReleaseBuffer(msgHeaderBuf)
			return nil, "", utils.WrapError("message header read failed", err)
		}

		msgType := MessageType(sb.Endianness.Uint16(msgHeaderBuf[0:2]))
		msgSize := sb.Endianness.Uint16(msgHeaderBuf[2:4])
		//nolint:gocritic // commentedOutCode: valid comment explaining unused field
		// msgFlags := msgHeaderBuf[4]  // Unused for now.
		utils.ReleaseBuffer(msgHeaderBuf)

		if msgSize == 0 {
			current += 8
			continue
		}

		// Read message data.
		data := utils.GetBuffer(int(msgSize))
		//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
		if _, err := r.ReadAt(data, int64(current+8)); err != nil {
			utils.ReleaseBuffer(data)
			return nil, "", utils.WrapError("message data read failed", err)
		}

		// Extract name if this is a name message.
		if msgType == MsgName && len(data) > 0 {
			// V1 name messages are null-terminated strings.
			nameBytes := data
			for i, b := range nameBytes {
				if b == 0 {
					nameBytes = nameBytes[:i]
					break
				}
			}
			name = string(nameBytes)
		}

		messages = append(messages, &HeaderMessage{
			Type:   msgType,
			Offset: current,
			Data:   data,
		})

		// Messages are 8-byte aligned in v1.
		msgTotalSize := 8 + uint64(msgSize)
		// Round up to next 8-byte boundary.
		if msgTotalSize%8 != 0 {
			msgTotalSize += 8 - (msgTotalSize % 8)
		}
		current += msgTotalSize
	}

	return messages, name, nil
}
