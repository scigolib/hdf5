package utils

import (
	"bytes"
	"errors"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBoundedReader covers reads inside, at and outside the declared file
// size, including the int64 overflow and negative-offset cases.
//
//nolint:gocognit // Table-driven test: one place lists every boundary case.
func TestBoundedReader(t *testing.T) {
	t.Parallel()

	const fileSize = 16
	payload := []byte("0123456789abcdef")

	tests := []struct {
		name    string
		off     int64
		length  int
		want    string
		wantErr bool
	}{
		{
			name:   "read at the start",
			off:    0,
			length: 4,
			want:   "0123",
		},
		{
			name:   "read in the middle",
			off:    4,
			length: 4,
			want:   "4567",
		},
		{
			name:   "read ends exactly at the file size",
			off:    12,
			length: 4,
			want:   "cdef",
		},
		{
			name:   "last byte",
			off:    15,
			length: 1,
			want:   "f",
		},
		{
			name:   "zero-length read at the start",
			off:    0,
			length: 0,
			want:   "",
		},
		{
			name:    "read starts at the file size",
			off:     fileSize,
			length:  1,
			wantErr: true,
		},
		{
			name:    "read starts before the end but ends past it",
			off:     14,
			length:  4,
			wantErr: true,
		},
		{
			name:    "one byte past the end",
			off:     fileSize + 1,
			length:  1,
			wantErr: true,
		},
		{
			name:   "zero-length read at the file size is allowed",
			off:    fileSize,
			length: 0,
			want:   "",
		},
		{
			name:    "zero-length read past the file size",
			off:     fileSize + 1,
			length:  0,
			wantErr: true,
		},
		{
			name:    "negative offset",
			off:     -1,
			length:  1,
			wantErr: true,
		},
		{
			name:    "negative offset with an empty buffer",
			off:     -1,
			length:  0,
			wantErr: true,
		},
		{
			name:    "offset near MaxInt64 with a length that overflows int64",
			off:     math.MaxInt64 - 2,
			length:  8,
			wantErr: true,
		},
		{
			name:    "offset equal to MaxInt64",
			off:     math.MaxInt64,
			length:  1,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := NewBoundedReader(bytes.NewReader(payload), fileSize)
			buf := make([]byte, tt.length)

			n, err := r.ReadAt(buf, tt.off)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("ReadAt(off=%d, len=%d) error = nil, want an error", tt.off, tt.length)
				}
				for _, want := range []string{"read beyond end of file", "fileSize=16"} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("ReadAt(off=%d, len=%d) error = %v, want error containing %q", tt.off, tt.length, err, want)
					}
				}
				if n != 0 {
					t.Errorf("ReadAt(off=%d, len=%d) n = %d, want 0", tt.off, tt.length, n)
				}
				return
			}

			if err != nil {
				t.Fatalf("ReadAt(off=%d, len=%d) error = %v, want nil", tt.off, tt.length, err)
			}
			if n != tt.length {
				t.Errorf("ReadAt(off=%d, len=%d) n = %d, want %d", tt.off, tt.length, n, tt.length)
			}
			if got := string(buf); got != tt.want {
				t.Errorf("ReadAt(off=%d, len=%d) = %q, want %q", tt.off, tt.length, got, tt.want)
			}
		})
	}
}

// TestBoundedReaderUnderlyingShortRead verifies that a read inside the declared
// size is delegated and the underlying reader's error is reported unchanged.
func TestBoundedReaderUnderlyingShortRead(t *testing.T) {
	t.Parallel()

	// The underlying reader holds 5 bytes while the wrapper believes in 100:
	// only the underlying reader can detect this, and its error must surface.
	r := NewBoundedReader(bytes.NewReader([]byte("short")), 100)

	n, err := r.ReadAt(make([]byte, 4), 10)
	if n != 0 {
		t.Errorf("ReadAt() n = %d, want 0", n)
	}
	if !errors.Is(err, io.EOF) {
		t.Errorf("ReadAt() error = %v, want io.EOF", err)
	}
}

// TestBoundedReaderNilUnderlying verifies that a nil or typed-nil reader fails
// with an error instead of panicking.
func TestBoundedReaderNilUnderlying(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		r    io.ReaderAt
	}{
		{name: "nil reader", r: nil},
		{name: "typed nil pointer", r: (*bytes.Reader)(nil)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := NewBoundedReader(tt.r, 16)
			n, err := r.ReadAt(make([]byte, 4), 0)

			if err == nil {
				t.Fatal("ReadAt() error = nil, want an error")
			}
			if n != 0 {
				t.Errorf("ReadAt() n = %d, want 0", n)
			}
		})
	}
}

// TestBoundedReaderEndOfFile verifies that the wrapper reports the size it
// enforces, so ReaderSize and the region checks use the real file size rather
// than falling back to an untrusted superblock value.
func TestBoundedReaderEndOfFile(t *testing.T) {
	r := NewBoundedReader(bytes.NewReader(make([]byte, 10)), 10)

	size, ok := ReaderSize(r)
	require.True(t, ok)
	require.Equal(t, uint64(10), size)

	// The concrete type exposes the size directly as well.
	eof, ok := r.(interface{ EndOfFile() uint64 })
	require.True(t, ok)
	require.Equal(t, uint64(10), eof.EndOfFile())
}
