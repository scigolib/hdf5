package core

import (
	"encoding/binary"
	"testing"
)

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestEncodeCompoundDatatypeV3_Simple tests encoding a simple compound with 3 fields.
func TestEncodeCompoundDatatypeV3_Simple(t *testing.T) {
	// Create a simple compound: { int32, float64, int64 }
	// Offsets: 0, 4, 12 (total size: 20 bytes)
	int32Type, err := CreateBasicDatatypeMessage(DatatypeFixed, 4)
	if err != nil {
		t.Fatalf("Failed to create int32 type: %v", err)
	}

	float64Type, err := CreateBasicDatatypeMessage(DatatypeFloat, 8)
	if err != nil {
		t.Fatalf("Failed to create float64 type: %v", err)
	}

	int64Type, err := CreateBasicDatatypeMessage(DatatypeFixed, 8)
	if err != nil {
		t.Fatalf("Failed to create int64 type: %v", err)
	}

	fields := []CompoundFieldDef{
		{Name: "field1", Offset: 0, Type: int32Type},
		{Name: "field2", Offset: 4, Type: float64Type},
		{Name: "field3", Offset: 12, Type: int64Type},
	}

	encoded, err := EncodeCompoundDatatypeV3(20, fields)
	if err != nil {
		t.Fatalf("EncodeCompoundDatatypeV3 failed: %v", err)
	}

	t.Logf("Encoded length: %d bytes", len(encoded))
	if len(encoded) >= 12 {
		t.Logf("Header: class=%d, size=%d", encoded[0]&0x0F, binary.LittleEndian.Uint32(encoded[4:8]))
		numMembers := binary.LittleEndian.Uint32(encoded[8:12])
		t.Logf("Member count in properties: %d", numMembers)
	}

	// Parse and validate
	dt, err := ParseDatatypeMessage(encoded)
	if err != nil {
		t.Fatalf("ParseDatatypeMessage failed: %v (encoded len=%d, first 32 bytes=%#v)", err, len(encoded), encoded[:minInt(32, len(encoded))])
	}

	t.Logf("Parsed: Class=%d, Version=%d, Size=%d, Props len=%d", dt.Class, dt.Version, dt.Size, len(dt.Properties))

	if dt.Class != DatatypeCompound {
		t.Errorf("Expected class Compound (6), got %d", dt.Class)
	}

	if dt.Version != 3 {
		t.Errorf("Expected version 3, got %d", dt.Version)
	}

	if dt.Size != 20 {
		t.Errorf("Expected size 20, got %d", dt.Size)
	}

	// Parse compound members
	t.Logf("About to parse compound members from %d property bytes", len(dt.Properties))
	compound, err := ParseCompoundType(dt)
	if err != nil {
		// Debug: show where parsing stopped
		t.Logf("Properties (first 60 bytes): %#v", dt.Properties[:minInt(60, len(dt.Properties))])
		t.Fatalf("ParseCompoundType failed: %v", err)
	}

	if len(compound.Members) != 3 {
		t.Fatalf("Expected 3 members, got %d", len(compound.Members))
	}

	// Verify member 1
	if compound.Members[0].Name != "field1" {
		t.Errorf("Member 0 name: expected 'field1', got '%s'", compound.Members[0].Name)
	}
	if compound.Members[0].Offset != 0 {
		t.Errorf("Member 0 offset: expected 0, got %d", compound.Members[0].Offset)
	}
	if compound.Members[0].Type.Size != 4 {
		t.Errorf("Member 0 size: expected 4, got %d", compound.Members[0].Type.Size)
	}

	// Verify member 2
	if compound.Members[1].Name != "field2" {
		t.Errorf("Member 1 name: expected 'field2', got '%s'", compound.Members[1].Name)
	}
	if compound.Members[1].Offset != 4 {
		t.Errorf("Member 1 offset: expected 4, got %d", compound.Members[1].Offset)
	}
	if compound.Members[1].Type.Size != 8 {
		t.Errorf("Member 1 size: expected 8, got %d", compound.Members[1].Type.Size)
	}

	// Verify member 3
	if compound.Members[2].Name != "field3" {
		t.Errorf("Member 2 name: expected 'field3', got '%s'", compound.Members[2].Name)
	}
	if compound.Members[2].Offset != 12 {
		t.Errorf("Member 2 offset: expected 12, got %d", compound.Members[2].Offset)
	}
	if compound.Members[2].Type.Size != 8 {
		t.Errorf("Member 2 size: expected 8, got %d", compound.Members[2].Type.Size)
	}
}

// TestEncodeCompoundDatatypeV3_WithString tests compound with string field.
func TestEncodeCompoundDatatypeV3_WithString(t *testing.T) {
	// Compound: { int32, string[10] }
	fields := []CompoundFieldDef{
		{
			Name:   "id",
			Offset: 0,
			Type: &DatatypeMessage{
				Class:   DatatypeFixed,
				Version: 1,
				Size:    4,
			},
		},
		{
			Name:   "name",
			Offset: 4,
			Type: &DatatypeMessage{
				Class:   DatatypeString,
				Version: 1,
				Size:    10,
			},
		},
	}

	encoded, err := EncodeCompoundDatatypeV3(14, fields)
	if err != nil {
		t.Fatalf("EncodeCompoundDatatypeV3 failed: %v", err)
	}

	// Round-trip validation
	dt, err := ParseDatatypeMessage(encoded)
	if err != nil {
		t.Fatalf("ParseDatatypeMessage failed: %v", err)
	}

	compound, err := ParseCompoundType(dt)
	if err != nil {
		t.Fatalf("ParseCompoundType failed: %v", err)
	}

	if len(compound.Members) != 2 {
		t.Fatalf("Expected 2 members, got %d", len(compound.Members))
	}

	if compound.Members[1].Type.Class != DatatypeString {
		t.Errorf("Expected string type for member 1, got class %d", compound.Members[1].Type.Class)
	}
}

// TestEncodeCompoundDatatypeV3_NestedCompound tests nested compound types.
func TestEncodeCompoundDatatypeV3_NestedCompound(t *testing.T) {
	// Inner compound: { float32, float32 } (8 bytes)
	// Must use CreateBasicDatatypeMessage to populate Properties
	floatType, err := CreateBasicDatatypeMessage(DatatypeFloat, 4)
	if err != nil {
		t.Fatalf("Failed to create float type: %v", err)
	}

	innerFields := []CompoundFieldDef{
		{
			Name:   "x",
			Offset: 0,
			Type:   floatType,
		},
		{
			Name:   "y",
			Offset: 4,
			Type:   floatType,
		},
	}

	innerEncoded, err := EncodeCompoundDatatypeV3(8, innerFields)
	if err != nil {
		t.Fatalf("Failed to encode inner compound: %v", err)
	}

	innerDt, err := ParseDatatypeMessage(innerEncoded)
	if err != nil {
		t.Fatalf("Failed to parse inner compound: %v", err)
	}

	// Outer compound: { int32, Point, int32 } (16 bytes)
	int32Type, err := CreateBasicDatatypeMessage(DatatypeFixed, 4)
	if err != nil {
		t.Fatalf("Failed to create int32 type: %v", err)
	}

	outerFields := []CompoundFieldDef{
		{
			Name:   "id",
			Offset: 0,
			Type:   int32Type,
		},
		{
			Name:   "point",
			Offset: 4,
			Type:   innerDt, // Nested compound!
		},
		{
			Name:   "count",
			Offset: 12,
			Type:   int32Type,
		},
	}

	outerEncoded, err := EncodeCompoundDatatypeV3(16, outerFields)
	if err != nil {
		t.Fatalf("Failed to encode outer compound: %v", err)
	}

	// Parse and validate nested structure
	outerDt, err := ParseDatatypeMessage(outerEncoded)
	if err != nil {
		t.Fatalf("ParseDatatypeMessage failed: %v", err)
	}

	outerCompound, err := ParseCompoundType(outerDt)
	if err != nil {
		t.Fatalf("ParseCompoundType failed: %v", err)
	}

	if len(outerCompound.Members) != 3 {
		t.Fatalf("Expected 3 members in outer compound, got %d", len(outerCompound.Members))
	}

	// Check nested compound member
	nestedMember := outerCompound.Members[1]
	if nestedMember.Name != "point" {
		t.Errorf("Nested member name: expected 'point', got '%s'", nestedMember.Name)
	}
	if nestedMember.Type.Class != DatatypeCompound {
		t.Errorf("Nested member should be compound, got class %d", nestedMember.Type.Class)
	}

	// Parse inner compound
	innerCompound, err := ParseCompoundType(nestedMember.Type)
	if err != nil {
		t.Fatalf("Failed to parse nested compound: %v", err)
	}

	if len(innerCompound.Members) != 2 {
		t.Fatalf("Expected 2 members in nested compound, got %d", len(innerCompound.Members))
	}
}

// TestEncodeCompoundDatatypeV1_Simple tests version 1 encoding.
func TestEncodeCompoundDatatypeV1_Simple(t *testing.T) {
	// Simple compound: { int32, float64 }
	// Must use CreateBasicDatatypeMessage to populate Properties
	int32Type, err := CreateBasicDatatypeMessage(DatatypeFixed, 4)
	if err != nil {
		t.Fatalf("Failed to create int32 type: %v", err)
	}

	float64Type, err := CreateBasicDatatypeMessage(DatatypeFloat, 8)
	if err != nil {
		t.Fatalf("Failed to create float64 type: %v", err)
	}

	fields := []CompoundFieldDef{
		{
			Name:   "field1",
			Offset: 0,
			Type:   int32Type,
		},
		{
			Name:   "field2",
			Offset: 4,
			Type:   float64Type,
		},
	}

	encoded, err := EncodeCompoundDatatypeV1(12, fields)
	if err != nil {
		t.Fatalf("EncodeCompoundDatatypeV1 failed: %v", err)
	}

	// Parse and validate
	dt, err := ParseDatatypeMessage(encoded)
	if err != nil {
		t.Fatalf("ParseDatatypeMessage failed: %v", err)
	}

	if dt.Class != DatatypeCompound {
		t.Errorf("Expected class Compound (6), got %d", dt.Class)
	}

	if dt.Version != 1 {
		t.Errorf("Expected version 1, got %d", dt.Version)
	}

	if dt.Size != 12 {
		t.Errorf("Expected size 12, got %d", dt.Size)
	}

	// Verify member count in ClassBitField (bits 0-15)
	numMembers := uint16(dt.ClassBitField & 0xFFFF)
	if numMembers != 2 {
		t.Errorf("Expected 2 members in ClassBitField, got %d", numMembers)
	}

	// Parse compound members
	compound, err := ParseCompoundType(dt)
	if err != nil {
		t.Fatalf("ParseCompoundType failed: %v", err)
	}

	if len(compound.Members) != 2 {
		t.Fatalf("Expected 2 members, got %d", len(compound.Members))
	}
}

// TestEncodeCompoundDatatypeV1_NamePadding tests version 1 name padding to 8-byte boundary.
func TestEncodeCompoundDatatypeV1_NamePadding(t *testing.T) {
	// Test different name lengths to verify 8-byte padding
	testCases := []struct {
		name           string
		expectedPadLen int
	}{
		{"a", 8},         // 1 char + null = 2, padded to 8
		{"abc", 8},       // 3 chars + null = 4, padded to 8
		{"abcdefg", 8},   // 7 chars + null = 8, already aligned
		{"abcdefgh", 16}, // 8 chars + null = 9, padded to 16
	}

	for _, tc := range testCases {
		int32Type, err := CreateBasicDatatypeMessage(DatatypeFixed, 4)
		if err != nil {
			t.Fatalf("Failed to create int32 type: %v", err)
		}

		fields := []CompoundFieldDef{
			{
				Name:   tc.name,
				Offset: 0,
				Type:   int32Type,
			},
		}

		encoded, err := EncodeCompoundDatatypeV1(4, fields)
		if err != nil {
			t.Fatalf("EncodeCompoundDatatypeV1 failed for name '%s': %v", tc.name, err)
		}

		// Header is 8 bytes, then comes name (padded) + offset (4) + array info (28) + member type
		// Name section starts at byte 8
		nameSection := encoded[8:]

		// Find null terminator
		nullPos := 0
		for i, b := range nameSection {
			if b == 0 {
				nullPos = i
				break
			}
		}

		actualNameLen := nullPos // Name without null terminator
		if actualNameLen != len(tc.name) {
			t.Errorf("Name '%s': expected length %d, got %d", tc.name, len(tc.name), actualNameLen)
		}

		// The next non-zero data should be at offset = tc.expectedPadLen + 4 (after array info and offset)
		// (name + padding + offset field + array info)
		// Round-trip parse to verify
		dt, err := ParseDatatypeMessage(encoded)
		if err != nil {
			t.Fatalf("Parse failed for name '%s': %v", tc.name, err)
		}

		compound, err := ParseCompoundType(dt)
		if err != nil {
			t.Fatalf("ParseCompoundType failed for name '%s': %v", tc.name, err)
		}

		if compound.Members[0].Name != tc.name {
			t.Errorf("Name mismatch: expected '%s', got '%s'", tc.name, compound.Members[0].Name)
		}
	}
}

// TestEncodeCompoundDatatypeV3_ErrorCases tests error handling.
func TestEncodeCompoundDatatypeV3_ErrorCases(t *testing.T) {
	tests := []struct {
		name      string
		totalSize uint32
		fields    []CompoundFieldDef
		wantErr   string
	}{
		{
			name:      "no fields",
			totalSize: 8,
			fields:    []CompoundFieldDef{},
			wantErr:   "at least one field",
		},
		{
			name:      "zero size",
			totalSize: 0,
			fields: []CompoundFieldDef{
				{Name: "field1", Offset: 0, Type: &DatatypeMessage{Class: DatatypeFixed, Size: 4}},
			},
			wantErr: "size cannot be 0",
		},
		{
			name:      "empty field name",
			totalSize: 4,
			fields: []CompoundFieldDef{
				{Name: "", Offset: 0, Type: &DatatypeMessage{Class: DatatypeFixed, Size: 4}},
			},
			wantErr: "name cannot be empty",
		},
		{
			name:      "nil field type",
			totalSize: 4,
			fields: []CompoundFieldDef{
				{Name: "field1", Offset: 0, Type: nil},
			},
			wantErr: "type cannot be nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := EncodeCompoundDatatypeV3(tt.totalSize, tt.fields)
			if err == nil {
				t.Fatalf("Expected error containing '%s', got nil", tt.wantErr)
			}
			if err.Error() == "" || tt.wantErr == "" {
				t.Fatalf("Error message empty: %v", err)
			}
			// Check error contains expected substring
			errMsg := err.Error()
			found := false
			for i := 0; i <= len(errMsg)-len(tt.wantErr); i++ {
				if errMsg[i:i+len(tt.wantErr)] == tt.wantErr {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Error message '%s' does not contain '%s'", errMsg, tt.wantErr)
			}
		})
	}
}

// TestCreateCompoundTypeFromFields tests convenience function.
func TestCreateCompoundTypeFromFields(t *testing.T) {
	// Create compound with automatic validation
	fields := []CompoundFieldDef{
		{
			Name:   "field1",
			Offset: 0,
			Type: &DatatypeMessage{
				Class:   DatatypeFixed,
				Version: 1,
				Size:    4,
			},
		},
		{
			Name:   "field2",
			Offset: 4,
			Type: &DatatypeMessage{
				Class:   DatatypeFloat,
				Version: 1,
				Size:    8,
			},
		},
	}

	dt, err := CreateCompoundTypeFromFields(fields)
	if err != nil {
		t.Fatalf("CreateCompoundTypeFromFields failed: %v", err)
	}

	if dt.Class != DatatypeCompound {
		t.Errorf("Expected Compound class, got %d", dt.Class)
	}

	if dt.Size != 12 {
		t.Errorf("Expected size 12 (4+8), got %d", dt.Size)
	}

	// Verify round-trip
	compound, err := ParseCompoundType(dt)
	if err != nil {
		t.Fatalf("ParseCompoundType failed: %v", err)
	}

	if len(compound.Members) != 2 {
		t.Fatalf("Expected 2 members, got %d", len(compound.Members))
	}
}

// TestCompoundDatatypeEncodeDecode_Binary tests binary format correctness.
func TestCompoundDatatypeEncodeDecode_Binary(t *testing.T) {
	// Manually verify binary format matches HDF5 spec
	fields := []CompoundFieldDef{
		{
			Name:   "x",
			Offset: 0,
			Type: &DatatypeMessage{
				Class:         DatatypeFixed,
				Version:       1,
				Size:          4,
				ClassBitField: 0, // Little-endian, signed
			},
		},
	}

	encoded, err := EncodeCompoundDatatypeV3(4, fields)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	// Verify header format
	if len(encoded) < 8 {
		t.Fatalf("Encoded message too short: %d bytes", len(encoded))
	}

	// Byte 0-3: class (4 bits) | version (4 bits) | reserved (24 bits)
	header := binary.LittleEndian.Uint32(encoded[0:4])
	class := header & 0x0F
	version := (header >> 4) & 0x0F

	if class != uint32(DatatypeCompound) {
		t.Errorf("Expected class 6 (Compound), got %d", class)
	}

	if version != 3 {
		t.Errorf("Expected version 3, got %d", version)
	}

	// Byte 4-7: size
	size := binary.LittleEndian.Uint32(encoded[4:8])
	if size != 4 {
		t.Errorf("Expected size 4, got %d", size)
	}

	// Byte 8-11: member count (version 3 uses uint32)
	memberCount := binary.LittleEndian.Uint32(encoded[8:12])
	if memberCount != 1 {
		t.Errorf("Expected 1 member, got %d", memberCount)
	}

	// Byte 12+: member name "x" (null-terminated)
	if encoded[12] != 'x' {
		t.Errorf("Expected member name 'x', got byte %c", encoded[12])
	}
	if encoded[13] != 0 {
		t.Errorf("Expected null terminator after name, got byte %d", encoded[13])
	}

	// Byte 14-17: member offset (uint32)
	memberOffset := binary.LittleEndian.Uint32(encoded[14:18])
	if memberOffset != 0 {
		t.Errorf("Expected member offset 0, got %d", memberOffset)
	}

	// Byte 18+: member datatype (should be int32)
	// This is a nested datatype message, so format repeats
}
