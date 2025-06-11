package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

const (
	UndefinedAddress = 0xFFFFFFFFFFFFFFFF
)

// SuperblockV0Reader reads and validates a V0 superblock
type SuperblockV0Reader struct {
	VersionSuperblock    uint8
	VersionFreeStorage   uint8
	VersionRootGroup     uint8
	Reserved1            uint8
	VersionSharedHeader  uint8
	SizeOfOffsets        uint8
	SizeOfLengths        uint8
	Reserved2            uint8
	GroupLeafNodeK       uint16
	GroupInternalNodeK   uint16
	FileConsistencyFlags uint32
	BaseAddress          uint64
	FreeSpaceInfoAddress uint64
	EndOfFileAddress     uint64
	DriverInfoAddress    uint64
	RootGroupAddress     uint64
}

func (sb *SuperblockV0Reader) Read(r io.ReadSeeker) error {
	// Read fixed fields
	if err := binary.Read(r, binary.LittleEndian, &sb.VersionSuperblock); err != nil {
		return fmt.Errorf("failed to read superblock version: %v", err)
	}

	if err := binary.Read(r, binary.LittleEndian, &sb.VersionFreeStorage); err != nil {
		return fmt.Errorf("failed to read free storage version: %v", err)
	}

	if err := binary.Read(r, binary.LittleEndian, &sb.VersionRootGroup); err != nil {
		return fmt.Errorf("failed to read root group version: %v", err)
	}

	if err := binary.Read(r, binary.LittleEndian, &sb.Reserved1); err != nil {
		return fmt.Errorf("failed to read reserved1: %v", err)
	}

	if err := binary.Read(r, binary.LittleEndian, &sb.VersionSharedHeader); err != nil {
		return fmt.Errorf("failed to read shared header version: %v", err)
	}

	if err := binary.Read(r, binary.LittleEndian, &sb.SizeOfOffsets); err != nil {
		return fmt.Errorf("failed to read sizeof offsets: %v", err)
	}

	if err := binary.Read(r, binary.LittleEndian, &sb.SizeOfLengths); err != nil {
		return fmt.Errorf("failed to read sizeof lengths: %v", err)
	}

	// CRITICAL VALIDATION: Check for invalid sizes
	if sb.SizeOfOffsets == 0 || sb.SizeOfLengths == 0 {
		return fmt.Errorf("invalid sizes for version 0: offset=%d, length=%d",
			sb.SizeOfOffsets, sb.SizeOfLengths)
	}

	if sb.SizeOfOffsets != 4 && sb.SizeOfOffsets != 8 {
		return fmt.Errorf("invalid offset size: %d (must be 4 or 8)", sb.SizeOfOffsets)
	}

	if sb.SizeOfLengths != 4 && sb.SizeOfLengths != 8 {
		return fmt.Errorf("invalid length size: %d (must be 4 or 8)", sb.SizeOfLengths)
	}

	if err := binary.Read(r, binary.LittleEndian, &sb.Reserved2); err != nil {
		return fmt.Errorf("failed to read reserved2: %v", err)
	}

	if err := binary.Read(r, binary.LittleEndian, &sb.GroupLeafNodeK); err != nil {
		return fmt.Errorf("failed to read group leaf node K: %v", err)
	}

	if err := binary.Read(r, binary.LittleEndian, &sb.GroupInternalNodeK); err != nil {
		return fmt.Errorf("failed to read group internal node K: %v", err)
	}

	if err := binary.Read(r, binary.LittleEndian, &sb.FileConsistencyFlags); err != nil {
		return fmt.Errorf("failed to read file consistency flags: %v", err)
	}

	// Read addresses based on SizeOfOffsets
	if sb.SizeOfOffsets == 8 {
		// Read 8-byte addresses
		if err := binary.Read(r, binary.LittleEndian, &sb.BaseAddress); err != nil {
			return fmt.Errorf("failed to read base address: %v", err)
		}
		if err := binary.Read(r, binary.LittleEndian, &sb.FreeSpaceInfoAddress); err != nil {
			return fmt.Errorf("failed to read free space info address: %v", err)
		}
		if err := binary.Read(r, binary.LittleEndian, &sb.EndOfFileAddress); err != nil {
			return fmt.Errorf("failed to read end of file address: %v", err)
		}
		if err := binary.Read(r, binary.LittleEndian, &sb.DriverInfoAddress); err != nil {
			return fmt.Errorf("failed to read driver info address: %v", err)
		}
		if err := binary.Read(r, binary.LittleEndian, &sb.RootGroupAddress); err != nil {
			return fmt.Errorf("failed to read root group address: %v", err)
		}
	} else {
		// Read 4-byte addresses and extend to 8 bytes
		var addr32 uint32

		if err := binary.Read(r, binary.LittleEndian, &addr32); err != nil {
			return fmt.Errorf("failed to read base address: %v", err)
		}
		sb.BaseAddress = uint64(addr32)

		if err := binary.Read(r, binary.LittleEndian, &addr32); err != nil {
			return fmt.Errorf("failed to read free space info address: %v", err)
		}
		sb.FreeSpaceInfoAddress = uint64(addr32)

		if err := binary.Read(r, binary.LittleEndian, &addr32); err != nil {
			return fmt.Errorf("failed to read end of file address: %v", err)
		}
		sb.EndOfFileAddress = uint64(addr32)

		if err := binary.Read(r, binary.LittleEndian, &addr32); err != nil {
			return fmt.Errorf("failed to read driver info address: %v", err)
		}
		sb.DriverInfoAddress = uint64(addr32)

		if err := binary.Read(r, binary.LittleEndian, &addr32); err != nil {
			return fmt.Errorf("failed to read root group address: %v", err)
		}
		sb.RootGroupAddress = uint64(addr32)
	}

	return nil
}

func (sb *SuperblockV0Reader) Validate() error {
	// Validate that we have a valid root group address
	if sb.RootGroupAddress == UndefinedAddress {
		return fmt.Errorf("root group address is undefined (0xFFFFFFFFFFFFFFFF)")
	}

	if sb.RootGroupAddress == 0 {
		return fmt.Errorf("root group address is zero (invalid)")
	}

	return nil
}

func (sb *SuperblockV0Reader) String() string {
	return fmt.Sprintf(`Superblock V0:
  Version: %d
  Sizeof offsets: %d
  Sizeof lengths: %d
  Group leaf K: %d
  Group internal K: %d
  Base address: 0x%016X
  Free space address: 0x%016X
  EOF address: 0x%016X
  Driver info address: 0x%016X
  Root group address: 0x%016X`,
		sb.VersionSuperblock,
		sb.SizeOfOffsets,
		sb.SizeOfLengths,
		sb.GroupLeafNodeK,
		sb.GroupInternalNodeK,
		sb.BaseAddress,
		sb.FreeSpaceInfoAddress,
		sb.EndOfFileAddress,
		sb.DriverInfoAddress,
		sb.RootGroupAddress)
}

// Example usage
func main() {
	filename := "testdata/v0.h5"

	file, err := os.Open(filename)
	if err != nil {
		fmt.Printf("Cannot open file: %v\n", err)
		return
	}
	defer file.Close()

	// Skip HDF5 signature (8 bytes)
	if _, err := file.Seek(8, io.SeekStart); err != nil {
		fmt.Printf("Cannot seek past signature: %v\n", err)
		return
	}

	// Read and validate superblock
	sb := &SuperblockV0Reader{}
	if err := sb.Read(file); err != nil {
		fmt.Printf("Failed to read superblock: %v\n", err)
		return
	}

	fmt.Println("✅ Successfully read superblock:")
	fmt.Println(sb.String())

	// Validate addresses
	if err := sb.Validate(); err != nil {
		fmt.Printf("❌ Superblock validation failed: %v\n", err)
		fmt.Println("\n🔧 This means your file generator needs to write valid addresses instead of 0xFFFFFFFFFFFFFFFF")
		return
	}

	fmt.Println("✅ Superblock validation passed")

	// Now try to read the root group
	fmt.Printf("\n🔍 Attempting to read root group at address 0x%016X\n", sb.RootGroupAddress)

	if _, err := file.Seek(int64(sb.RootGroupAddress), io.SeekStart); err != nil {
		fmt.Printf("❌ Cannot seek to root group address: %v\n", err)
		return
	}

	// Try to read some data at the root group address
	buffer := make([]byte, 32)
	n, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		fmt.Printf("❌ Cannot read root group data: %v\n", err)
		return
	}

	fmt.Printf("📄 Root group data (%d bytes): %x\n", n, buffer[:n])
}
