package structures

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseLinkMessage_HardLink(t *testing.T) {
	tests := []struct {
		name             string
		data             []byte
		expectedName     string
		expectedAddr     uint64
		expectedVersion  uint8
		expectedFlags    uint8
		expectedCharset  uint8
		checkCreationOrd bool
		creationOrder    int64
	}{
		{
			name: "simple hard link - 1 byte name size",
			data: func() []byte {
				buf := make([]byte, 256)
				buf[0] = 1                                 // Version
				buf[1] = flagNameSize0 | flagStoreLinkType // Flags: 1-byte name size, store link type
				buf[2] = byte(LinkTypeHard)                // Link type
				buf[3] = 5                                 // Name length: 5
				copy(buf[4:9], "test1")                    // Name: "test1"
				// Object address (8 bytes for OffsetSize=8)
				binary.LittleEndian.PutUint64(buf[9:17], 0x1000)
				return buf
			}(),
			expectedName:    "test1",
			expectedAddr:    0x1000,
			expectedVersion: 1,
			expectedFlags:   flagNameSize0 | flagStoreLinkType,
			expectedCharset: 0,
		},
		{
			name: "hard link - 2 byte name size",
			data: func() []byte {
				buf := make([]byte, 256)
				buf[0] = 1                                 // Version
				buf[1] = flagNameSize1 | flagStoreLinkType // Flags: 2-byte name size
				buf[2] = byte(LinkTypeHard)
				binary.LittleEndian.PutUint16(buf[3:5], 10) // Name length: 10
				copy(buf[5:15], "longerName")
				binary.LittleEndian.PutUint64(buf[15:23], 0x2000)
				return buf
			}(),
			expectedName:    "longerName",
			expectedAddr:    0x2000,
			expectedVersion: 1,
			expectedFlags:   flagNameSize1 | flagStoreLinkType,
		},
		{
			name: "hard link with creation order",
			data: func() []byte {
				buf := make([]byte, 256)
				buf[0] = 1 // Version
				buf[1] = flagNameSize0 | flagStoreLinkType | flagStoreCreationOrder
				buf[2] = byte(LinkTypeHard)
				// Creation order (8 bytes)
				binary.LittleEndian.PutUint64(buf[3:11], 42)
				buf[11] = 4 // Name length
				copy(buf[12:16], "test")
				binary.LittleEndian.PutUint64(buf[16:24], 0x3000)
				return buf
			}(),
			expectedName:     "test",
			expectedAddr:     0x3000,
			expectedVersion:  1,
			expectedFlags:    flagNameSize0 | flagStoreLinkType | flagStoreCreationOrder,
			checkCreationOrd: true,
			creationOrder:    42,
		},
		{
			name: "hard link with charset",
			data: func() []byte {
				buf := make([]byte, 256)
				buf[0] = 1 // Version
				buf[1] = flagNameSize0 | flagStoreLinkType | flagStoreCharset
				buf[2] = byte(LinkTypeHard)
				buf[3] = 1  // Charset: UTF-8
				buf[4] = 24 // Name length (24 chars for the test name)
				copy(buf[5:29], "test_with_charset_flag!!")
				binary.LittleEndian.PutUint64(buf[29:37], 0x4000)
				return buf
			}(),
			expectedName:    "test_with_charset_flag!!",
			expectedAddr:    0x4000,
			expectedVersion: 1,
			expectedFlags:   flagNameSize0 | flagStoreLinkType | flagStoreCharset,
			expectedCharset: 1,
		},
		{
			name: "hard link without explicit link type flag",
			data: func() []byte {
				buf := make([]byte, 256)
				buf[0] = 1             // Version
				buf[1] = flagNameSize0 // Flags: no link type stored (defaults to hard)
				buf[2] = 7             // Name length
				copy(buf[3:10], "default")
				binary.LittleEndian.PutUint64(buf[10:18], 0x5000)
				return buf
			}(),
			expectedName:    "default",
			expectedAddr:    0x5000,
			expectedVersion: 1,
			expectedFlags:   flagNameSize0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sb := createMockSuperblock()
			msg, err := ParseLinkMessage(tt.data, sb)
			require.NoError(t, err)
			require.NotNil(t, msg)
			require.Equal(t, tt.expectedVersion, msg.Version)
			require.Equal(t, tt.expectedFlags, msg.Flags)
			require.Equal(t, LinkTypeHard, msg.Type)
			require.Equal(t, tt.expectedName, msg.Name)
			require.Equal(t, tt.expectedAddr, msg.ObjectAddress)
			if tt.expectedCharset != 0 {
				require.Equal(t, tt.expectedCharset, msg.CharacterSet)
			}
			if tt.checkCreationOrd {
				require.True(t, msg.CreationOrderValid)
				require.Equal(t, tt.creationOrder, msg.CreationOrder)
			}
			require.True(t, msg.IsHardLink())
			require.False(t, msg.IsSoftLink())
		})
	}
}

func TestParseLinkMessage_SoftLink(t *testing.T) {
	tests := []struct {
		name            string
		data            []byte
		expectedName    string
		expectedTarget  string
		expectedVersion uint8
	}{
		{
			name: "simple soft link",
			data: func() []byte {
				buf := make([]byte, 256)
				buf[0] = 1 // Version
				buf[1] = flagNameSize0 | flagStoreLinkType
				buf[2] = byte(LinkTypeSoft)
				buf[3] = 4 // Name length
				copy(buf[4:8], "link")
				// Soft link target length (2 bytes)
				binary.LittleEndian.PutUint16(buf[8:10], 11)
				copy(buf[10:21], "/path/to/ds")
				return buf
			}(),
			expectedName:    "link",
			expectedTarget:  "/path/to/ds",
			expectedVersion: 1,
		},
		{
			name: "soft link with long path",
			data: func() []byte {
				buf := make([]byte, 512)
				buf[0] = 1
				buf[1] = flagNameSize0 | flagStoreLinkType
				buf[2] = byte(LinkTypeSoft)
				buf[3] = 8
				copy(buf[4:12], "softlink")
				target := "/very/long/path/to/dataset/somewhere/deep/in/hierarchy"
				binary.LittleEndian.PutUint16(buf[12:14], uint16(len(target)))
				copy(buf[14:], target)
				return buf
			}(),
			expectedName:    "softlink",
			expectedTarget:  "/very/long/path/to/dataset/somewhere/deep/in/hierarchy",
			expectedVersion: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sb := createMockSuperblock()
			msg, err := ParseLinkMessage(tt.data, sb)
			require.NoError(t, err)
			require.NotNil(t, msg)
			require.Equal(t, tt.expectedVersion, msg.Version)
			require.Equal(t, LinkTypeSoft, msg.Type)
			require.Equal(t, tt.expectedName, msg.Name)
			require.Equal(t, tt.expectedTarget, msg.TargetPath)
			require.False(t, msg.IsHardLink())
			require.True(t, msg.IsSoftLink())
		})
	}
}

func TestParseLinkMessage_ExternalLink(t *testing.T) {
	// Test user-defined/external link (type >= 64)
	buf := make([]byte, 256)
	buf[0] = 1 // Version
	buf[1] = flagNameSize0 | flagStoreLinkType
	buf[2] = byte(LinkTypeExternal) // Type 64
	buf[3] = 8                      // Name length
	copy(buf[4:12], "external")
	// User-defined data length (2 bytes)
	binary.LittleEndian.PutUint16(buf[12:14], 20)
	// User-defined data (we skip parsing it)
	copy(buf[14:34], "external_link_data..")

	sb := createMockSuperblock()
	msg, err := ParseLinkMessage(buf, sb)
	require.NoError(t, err)
	require.NotNil(t, msg)
	require.Equal(t, LinkTypeExternal, msg.Type)
	require.Equal(t, "external", msg.Name)
	require.False(t, msg.IsHardLink())
	require.False(t, msg.IsSoftLink())
}

func TestParseLinkMessage_NameSizeVariants(t *testing.T) {
	tests := []struct {
		name         string
		nameSizeType uint8
		nameLen      uint64
		setupData    func([]byte, uint64)
	}{
		{
			name:         "1 byte name size",
			nameSizeType: flagNameSize0,
			nameLen:      10,
			setupData: func(buf []byte, nameLen uint64) {
				buf[3] = byte(nameLen)
			},
		},
		{
			name:         "2 byte name size",
			nameSizeType: flagNameSize1,
			nameLen:      256,
			setupData: func(buf []byte, nameLen uint64) {
				binary.LittleEndian.PutUint16(buf[3:5], uint16(nameLen))
			},
		},
		{
			name:         "4 byte name size",
			nameSizeType: flagNameSize2,
			nameLen:      70000,
			setupData: func(buf []byte, nameLen uint64) {
				binary.LittleEndian.PutUint32(buf[3:7], uint32(nameLen))
			},
		},
		{
			name:         "8 byte name size",
			nameSizeType: flagNameSize3,
			nameLen:      100000,
			setupData: func(buf []byte, nameLen uint64) {
				binary.LittleEndian.PutUint64(buf[3:11], nameLen)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := make([]byte, int(tt.nameLen)+200)
			buf[0] = 1 // Version
			buf[1] = tt.nameSizeType | flagStoreLinkType
			buf[2] = byte(LinkTypeHard)

			tt.setupData(buf, tt.nameLen)

			// Find where name starts
			var nameStart int
			switch tt.nameSizeType {
			case flagNameSize0:
				nameStart = 4
			case flagNameSize1:
				nameStart = 5
			case flagNameSize2:
				nameStart = 7
			case flagNameSize3:
				nameStart = 11
			}

			// Fill name with 'A'
			for i := 0; i < int(tt.nameLen); i++ {
				buf[nameStart+i] = 'A'
			}

			// Object address after name
			addrStart := nameStart + int(tt.nameLen)
			binary.LittleEndian.PutUint64(buf[addrStart:addrStart+8], 0x9999)

			sb := createMockSuperblock()
			msg, err := ParseLinkMessage(buf, sb)
			require.NoError(t, err)
			require.NotNil(t, msg)
			require.Len(t, msg.Name, int(tt.nameLen))
			require.Equal(t, uint64(0x9999), msg.ObjectAddress)
		})
	}
}

func TestParseLinkMessage_AllFlags(t *testing.T) {
	// Test all flags combined
	buf := make([]byte, 256)
	buf[0] = 1 // Version
	buf[1] = flagNameSize0 | flagStoreLinkType | flagStoreCreationOrder | flagStoreCharset
	buf[2] = byte(LinkTypeHard)
	// Creation order
	binary.LittleEndian.PutUint64(buf[3:11], 123)
	buf[11] = 1 // Charset
	buf[12] = 4 // Name length
	copy(buf[13:17], "test")
	binary.LittleEndian.PutUint64(buf[17:25], 0x8888)

	sb := createMockSuperblock()
	msg, err := ParseLinkMessage(buf, sb)
	require.NoError(t, err)
	require.NotNil(t, msg)
	require.True(t, msg.CreationOrderValid)
	require.Equal(t, int64(123), msg.CreationOrder)
	require.Equal(t, uint8(1), msg.CharacterSet)
	require.Equal(t, "test", msg.Name)
	require.Equal(t, uint64(0x8888), msg.ObjectAddress)
}

func TestParseLinkMessage_Errors(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr string
	}{
		{
			name:    "too short - no version",
			data:    []byte{},
			wantErr: "too short",
		},
		{
			name:    "too short - no flags",
			data:    []byte{0x01},
			wantErr: "too short",
		},
		{
			name: "unsupported version",
			data: func() []byte {
				buf := make([]byte, 256)
				buf[0] = 2 // Version 2 (unsupported)
				buf[1] = flagNameSize0
				return buf
			}(),
			wantErr: "unsupported link message version",
		},
		{
			name: "unexpected end reading link type",
			data: func() []byte {
				buf := make([]byte, 2)
				buf[0] = 1
				buf[1] = flagStoreLinkType // Flag set but no data
				return buf
			}(),
			wantErr: "unexpected end of data reading link type",
		},
		{
			name: "unexpected end reading creation order",
			data: func() []byte {
				buf := make([]byte, 3)
				buf[0] = 1
				buf[1] = flagStoreCreationOrder
				buf[2] = 0 // Not enough data for 8 bytes
				return buf
			}(),
			wantErr: "unexpected end of data reading creation order",
		},
		{
			name: "unexpected end reading charset",
			data: func() []byte {
				buf := make([]byte, 2)
				buf[0] = 1
				buf[1] = flagStoreCharset
				return buf
			}(),
			wantErr: "unexpected end of data reading charset",
		},
		{
			name: "unexpected end reading name length",
			data: func() []byte {
				buf := make([]byte, 2)
				buf[0] = 1
				buf[1] = flagNameSize0
				return buf
			}(),
			wantErr: "unexpected end of data reading name length",
		},
		{
			name: "zero name length",
			data: func() []byte {
				buf := make([]byte, 256)
				buf[0] = 1
				buf[1] = flagNameSize0
				buf[2] = 0 // Name length = 0
				return buf
			}(),
			wantErr: "invalid name length: 0",
		},
		{
			name: "unexpected end reading name",
			data: func() []byte {
				buf := make([]byte, 10)
				buf[0] = 1
				buf[1] = flagNameSize0
				buf[2] = 100 // Claims 100 bytes but buffer too small
				return buf
			}(),
			wantErr: "unexpected end of data reading name",
		},
		{
			name: "unexpected end reading object address",
			data: func() []byte {
				buf := make([]byte, 256)
				buf[0] = 1
				buf[1] = flagNameSize0 | flagStoreLinkType
				buf[2] = byte(LinkTypeHard)
				buf[3] = 4
				copy(buf[4:8], "test")
				// No space for 8-byte address
				return buf[:8]
			}(),
			wantErr: "unexpected end of data reading object address",
		},
		{
			name: "soft link - zero target length",
			data: func() []byte {
				buf := make([]byte, 256)
				buf[0] = 1
				buf[1] = flagNameSize0 | flagStoreLinkType
				buf[2] = byte(LinkTypeSoft)
				buf[3] = 4
				copy(buf[4:8], "link")
				binary.LittleEndian.PutUint16(buf[8:10], 0) // Target length = 0
				return buf
			}(),
			wantErr: "invalid soft link length: 0",
		},
		{
			name: "soft link - unexpected end reading target",
			data: func() []byte {
				buf := make([]byte, 20)
				buf[0] = 1
				buf[1] = flagNameSize0 | flagStoreLinkType
				buf[2] = byte(LinkTypeSoft)
				buf[3] = 4
				copy(buf[4:8], "link")
				binary.LittleEndian.PutUint16(buf[8:10], 100) // Claims 100 bytes
				return buf
			}(),
			wantErr: "unexpected end of data reading soft link path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sb := createMockSuperblock()
			msg, err := ParseLinkMessage(tt.data, sb)
			require.Error(t, err)
			require.Nil(t, msg)
			require.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestLinkMessage_String(t *testing.T) {
	tests := []struct {
		name     string
		msg      *LinkMessage
		expected string
	}{
		{
			name: "hard link",
			msg: &LinkMessage{
				Type:          LinkTypeHard,
				Name:          "dataset1",
				ObjectAddress: 0x1234,
			},
			expected: "Hard link 'dataset1' -> address 0x1234",
		},
		{
			name: "soft link",
			msg: &LinkMessage{
				Type:       LinkTypeSoft,
				Name:       "link1",
				TargetPath: "/path/to/target",
			},
			expected: "Soft link 'link1' -> '/path/to/target'",
		},
		{
			name: "external link",
			msg: &LinkMessage{
				Type: LinkTypeExternal,
				Name: "extlink",
			},
			expected: "Link 'extlink' (type 64)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.msg.String()
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestLinkMessage_TypeMethods(t *testing.T) {
	hardLink := &LinkMessage{Type: LinkTypeHard}
	require.True(t, hardLink.IsHardLink())
	require.False(t, hardLink.IsSoftLink())

	softLink := &LinkMessage{Type: LinkTypeSoft}
	require.False(t, softLink.IsHardLink())
	require.True(t, softLink.IsSoftLink())

	extLink := &LinkMessage{Type: LinkTypeExternal}
	require.False(t, extLink.IsHardLink())
	require.False(t, extLink.IsSoftLink())
}

func TestLinkType_Constants(t *testing.T) {
	require.Equal(t, LinkType(0), LinkTypeHard)
	require.Equal(t, LinkType(1), LinkTypeSoft)
	require.Equal(t, LinkType(64), LinkTypeExternal)
}

func BenchmarkParseLinkMessage_HardLink(b *testing.B) {
	buf := make([]byte, 256)
	buf[0] = 1
	buf[1] = flagNameSize0 | flagStoreLinkType
	buf[2] = byte(LinkTypeHard)
	buf[3] = 10
	copy(buf[4:14], "benchmark!")
	binary.LittleEndian.PutUint64(buf[14:22], 0x1000)

	sb := createMockSuperblock()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = ParseLinkMessage(buf, sb)
	}
}

func BenchmarkParseLinkMessage_SoftLink(b *testing.B) {
	buf := make([]byte, 256)
	buf[0] = 1
	buf[1] = flagNameSize0 | flagStoreLinkType
	buf[2] = byte(LinkTypeSoft)
	buf[3] = 8
	copy(buf[4:12], "softlink")
	binary.LittleEndian.PutUint16(buf[12:14], 20)
	copy(buf[14:34], "/path/to/target/data")

	sb := createMockSuperblock()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = ParseLinkMessage(buf, sb)
	}
}
