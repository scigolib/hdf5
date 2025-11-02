package core

import (
	"fmt"
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
//
// Continuation messages (type 0x0010) contain:
// - Address of continuation block (OffsetSize bytes).
// - Size of continuation block (LengthSize bytes).
// Reference: H5Ocont.c from HDF5 C library.
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
	// refCount := sb.Endianness.Uint32(headerBuf[4:8])  // Unused.
	headerSize := sb.Endianness.Uint32(headerBuf[8:12])

	// Messages start after the 16-byte header.
	current := headerAddr + 16
	end := headerAddr + uint64(headerSize)

	var messages []*HeaderMessage
	var name string

	// Parse messages in the main header block
	blockMessages, blockName, err := parseV1MessagesInBlock(r, current, end, numMessages, sb)
	if err != nil {
		return nil, "", err
	}
	messages = append(messages, blockMessages...)
	if blockName != "" {
		name = blockName
	}

	// Process continuation messages
	// We need to iterate through messages and follow any continuations
	continuations := findContinuations(messages, sb)
	for len(continuations) > 0 {
		cont := continuations[0]
		continuations = continuations[1:]

		// Parse continuation block
		contMessages, contName, err := parseV1ContinuationBlock(r, cont.Address, cont.Size, sb)
		if err != nil {
			return nil, "", utils.WrapError("continuation block parse failed", err)
		}

		// Add messages from continuation
		messages = append(messages, contMessages...)
		if contName != "" && name == "" {
			name = contName
		}

		// Check if continuation block has more continuations
		newConts := findContinuations(contMessages, sb)
		continuations = append(continuations, newConts...)
	}

	return messages, name, nil
}

// continuationInfo holds information about a continuation block.
type continuationInfo struct {
	Address uint64
	Size    uint64
}

// findContinuations extracts continuation block information from messages.
func findContinuations(messages []*HeaderMessage, sb *Superblock) []continuationInfo {
	var continuations []continuationInfo
	for _, msg := range messages {
		if msg.Type == MsgContinuation && len(msg.Data) > 0 {
			// Parse continuation message data
			// Format: Address (OffsetSize bytes) + Size (LengthSize bytes)
			// Reference: H5Ocont.c decode function
			cont, err := parseContinuationMessage(msg.Data, sb)
			if err != nil {
				// Skip invalid continuation messages
				continue
			}
			continuations = append(continuations, cont)
		}
	}
	return continuations
}

// parseContinuationMessage extracts address and size from continuation message data.
// Continuation message format (from H5Ocont.c):
// - Address of continuation block (OffsetSize bytes from superblock).
// - Size of continuation block (LengthSize bytes from superblock).
func parseContinuationMessage(data []byte, sb *Superblock) (continuationInfo, error) {
	minSize := int(sb.OffsetSize + sb.LengthSize)
	if len(data) < minSize {
		return continuationInfo{}, fmt.Errorf("continuation message too small: need %d bytes, got %d", minSize, len(data))
	}

	offset := 0

	// Decode address (OffsetSize bytes)
	var address uint64
	switch sb.OffsetSize {
	case 1:
		address = uint64(data[offset])
	case 2:
		address = uint64(sb.Endianness.Uint16(data[offset : offset+2]))
	case 4:
		address = uint64(sb.Endianness.Uint32(data[offset : offset+4]))
	case 8:
		address = sb.Endianness.Uint64(data[offset : offset+8])
	default:
		return continuationInfo{}, fmt.Errorf("invalid offset size: %d", sb.OffsetSize)
	}
	offset += int(sb.OffsetSize)

	// Decode size (LengthSize bytes)
	var size uint64
	switch sb.LengthSize {
	case 1:
		size = uint64(data[offset])
	case 2:
		size = uint64(sb.Endianness.Uint16(data[offset : offset+2]))
	case 4:
		size = uint64(sb.Endianness.Uint32(data[offset : offset+4]))
	case 8:
		size = sb.Endianness.Uint64(data[offset : offset+8])
	default:
		return continuationInfo{}, fmt.Errorf("invalid length size: %d", sb.LengthSize)
	}

	// Validate continuation
	if size == 0 {
		return continuationInfo{}, fmt.Errorf("invalid continuation block size: 0")
	}

	return continuationInfo{
		Address: address,
		Size:    size,
	}, nil
}

// parseV1ContinuationBlock parses messages from a continuation block.
func parseV1ContinuationBlock(r io.ReaderAt, blockAddr, blockSize uint64, sb *Superblock) ([]*HeaderMessage, string, error) {
	// V1 continuation blocks don't have a header, just messages
	// They continue with the same message format as the main header
	current := blockAddr
	end := blockAddr + blockSize

	// Count messages in this block by scanning
	// V1 continuations don't have a message count, so we parse until end of block
	return parseV1MessagesInBlock(r, current, end, 0xFFFF, sb) // Large number to parse all messages
}

// parseV1MessagesInBlock parses messages from a block (header or continuation).
func parseV1MessagesInBlock(r io.ReaderAt, start, end uint64, maxMessages uint16, sb *Superblock) ([]*HeaderMessage, string, error) {
	var messages []*HeaderMessage
	var name string
	current := start
	messageCount := uint16(0)

	for current < end {
		// Check if we've read enough messages
		if messageCount >= maxMessages {
			break
		}

		// Check if we have enough space for message header
		if current+8 > end {
			break
		}

		// Read message header (8 bytes).
		msgHeaderBuf := utils.GetBuffer(8)
		//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
		if _, err := r.ReadAt(msgHeaderBuf, int64(current)); err != nil {
			utils.ReleaseBuffer(msgHeaderBuf)
			if err == io.EOF {
				break // End of block reached
			}
			return nil, "", utils.WrapError("message header read failed", err)
		}

		msgType := MessageType(sb.Endianness.Uint16(msgHeaderBuf[0:2]))
		msgSize := sb.Endianness.Uint16(msgHeaderBuf[2:4])
		// msgFlags := msgHeaderBuf[4]  // Unused for now.
		utils.ReleaseBuffer(msgHeaderBuf)

		if msgSize == 0 {
			// Zero-size message, skip it but don't count it
			current += 8
			continue
		}

		// Check if we have enough space for message data
		if current+8+uint64(msgSize) > end {
			break
		}

		// Read message data.
		data := utils.GetBuffer(int(msgSize))
		//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
		if _, err := r.ReadAt(data, int64(current+8)); err != nil {
			utils.ReleaseBuffer(data)
			if err == io.EOF {
				break
			}
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

		// Count this as a parsed message
		messageCount++
	}

	return messages, name, nil
}
