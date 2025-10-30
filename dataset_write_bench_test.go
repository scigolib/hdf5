package hdf5

import (
	"testing"
)

// BenchmarkGetDatatypeInfo_Basic benchmarks basic type lookup (post-registry pattern).
func BenchmarkGetDatatypeInfo_Basic(b *testing.B) {
	config := &datasetConfig{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = getDatatypeInfo(Int32, config)
	}
}

// BenchmarkGetDatatypeInfo_String benchmarks string type lookup with validation.
func BenchmarkGetDatatypeInfo_String(b *testing.B) {
	config := &datasetConfig{stringSize: 32}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = getDatatypeInfo(String, config)
	}
}

// BenchmarkGetDatatypeInfo_Array benchmarks array type lookup (with recursion).
func BenchmarkGetDatatypeInfo_Array(b *testing.B) {
	config := &datasetConfig{arrayDims: []uint64{3, 4}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = getDatatypeInfo(ArrayInt32, config)
	}
}

// BenchmarkGetDatatypeInfo_Enum benchmarks enum type lookup (with recursion).
func BenchmarkGetDatatypeInfo_Enum(b *testing.B) {
	config := &datasetConfig{
		enumNames:  []string{"Red", "Green", "Blue"},
		enumValues: []int64{0, 1, 2},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = getDatatypeInfo(EnumInt32, config)
	}
}

// BenchmarkEncodeDatatypeMessage_Basic benchmarks basic type encoding.
func BenchmarkEncodeDatatypeMessage_Basic(b *testing.B) {
	config := &datasetConfig{}
	info, _ := getDatatypeInfo(Int32, config)
	handler := datatypeRegistry[Int32]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = handler.EncodeDatatypeMessage(info)
	}
}

// BenchmarkEncodeDatatypeMessage_Array benchmarks array type encoding.
func BenchmarkEncodeDatatypeMessage_Array(b *testing.B) {
	config := &datasetConfig{arrayDims: []uint64{3, 4}}
	info, _ := getDatatypeInfo(ArrayInt32, config)
	handler := datatypeRegistry[ArrayInt32]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = handler.EncodeDatatypeMessage(info)
	}
}

// BenchmarkEncodeDatatypeMessage_Enum benchmarks enum type encoding.
func BenchmarkEncodeDatatypeMessage_Enum(b *testing.B) {
	config := &datasetConfig{
		enumNames:  []string{"Red", "Green", "Blue"},
		enumValues: []int64{0, 1, 2},
	}
	info, _ := getDatatypeInfo(EnumInt32, config)
	handler := datatypeRegistry[EnumInt32]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = handler.EncodeDatatypeMessage(info)
	}
}

// BenchmarkRegistry_Lookup benchmarks the registry map lookup performance.
func BenchmarkRegistry_Lookup(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = datatypeRegistry[Int32]
	}
}
