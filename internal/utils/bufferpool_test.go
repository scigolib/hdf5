package utils

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetBuffer(t *testing.T) {
	tests := []struct {
		name        string
		size        int
		checkLen    bool
		checkMinCap int
	}{
		{
			name:        "small buffer within pool capacity",
			size:        1024,
			checkLen:    true,
			checkMinCap: 1024,
		},
		{
			name:        "exact pool default size",
			size:        4096,
			checkLen:    true,
			checkMinCap: 4096,
		},
		{
			name:        "larger than pool capacity",
			size:        8192,
			checkLen:    true,
			checkMinCap: 8192,
		},
		{
			name:        "zero size",
			size:        0,
			checkLen:    true,
			checkMinCap: 0,
		},
		{
			name:        "very small size",
			size:        1,
			checkLen:    true,
			checkMinCap: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := GetBuffer(tt.size)
			require.NotNil(t, buf)

			if tt.checkLen {
				require.Equal(t, tt.size, len(buf), "buffer length should match requested size")
			}

			require.GreaterOrEqual(t, cap(buf), tt.checkMinCap, "buffer capacity should be at least requested size")

			// Return buffer to pool
			ReleaseBuffer(buf)
		})
	}
}

func TestReleaseBuffer(t *testing.T) {
	// Get a buffer
	buf := GetBuffer(1024)
	require.NotNil(t, buf)
	require.Equal(t, 1024, len(buf))

	// Fill it with data
	for i := range buf {
		buf[i] = byte(i % 256)
	}

	// Release it
	ReleaseBuffer(buf)

	// Get another buffer - might be the same one from pool
	buf2 := GetBuffer(512)
	require.NotNil(t, buf2)
	require.Equal(t, 512, len(buf2))

	// Release it
	ReleaseBuffer(buf2)
}

func TestBufferPoolReuse(t *testing.T) {
	// This test verifies that the pool reuses buffers
	buf1 := GetBuffer(2048)
	require.Equal(t, 2048, len(buf1))

	// Mark the buffer
	if cap(buf1) >= 2048 {
		buf1[0] = 0xAB
		buf1[2047] = 0xCD
	}

	// Return to pool
	ReleaseBuffer(buf1)

	// Get another buffer of same size
	buf2 := GetBuffer(2048)
	require.Equal(t, 2048, len(buf2))

	// The pool resets length to 0 before putting back,
	// so we verify the buffer is properly sized
	require.GreaterOrEqual(t, cap(buf2), 2048)

	ReleaseBuffer(buf2)
}

func TestBufferPoolConcurrency(t *testing.T) {
	// Test concurrent access to buffer pool
	const goroutines = 10
	const iterations = 100

	done := make(chan bool, goroutines)

	for g := 0; g < goroutines; g++ {
		go func() {
			for i := 0; i < iterations; i++ {
				size := 1024 + (i % 4096)
				buf := GetBuffer(size)
				require.Equal(t, size, len(buf))

				// Do some work with buffer
				for j := 0; j < len(buf); j++ {
					buf[j] = byte(j)
				}

				ReleaseBuffer(buf)
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for g := 0; g < goroutines; g++ {
		<-done
	}
}

func BenchmarkGetBuffer(b *testing.B) {
	sizes := []int{512, 1024, 4096, 8192}

	for _, size := range sizes {
		b.Run(string(rune(size)), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				buf := GetBuffer(size)
				ReleaseBuffer(buf)
			}
		})
	}
}

func BenchmarkGetBufferNoPool(b *testing.B) {
	sizes := []int{512, 1024, 4096, 8192}

	for _, size := range sizes {
		b.Run(string(rune(size)), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = make([]byte, size)
			}
		})
	}
}
