package utils

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// noSizeReader is an io.ReaderAt that cannot report its size.
type noSizeReader struct{}

func (noSizeReader) ReadAt([]byte, int64) (int, error) { return 0, io.EOF }

// statErrorReader mimics a closed or otherwise unusable file handle.
type statErrorReader struct{}

func (statErrorReader) ReadAt([]byte, int64) (int, error) { return 0, io.EOF }

func (statErrorReader) Stat() (fs.FileInfo, error) { return nil, errors.New("stat failed") }

// negativeSizeReader reports a negative size, which cannot describe a byte range.
type negativeSizeReader struct{}

func (negativeSizeReader) ReadAt([]byte, int64) (int, error) { return 0, io.EOF }

func (negativeSizeReader) Size() int64 { return -1 }

// logicalEOFReader models a writer: the file is allocated to eof but the
// physical view may be shorter. EndOfFile must win over Size.
type logicalEOFReader struct {
	eof uint64
}

func (logicalEOFReader) ReadAt([]byte, int64) (int, error) { return 0, io.EOF }

func (logicalEOFReader) Size() int64 { return 1 }

func (r logicalEOFReader) EndOfFile() uint64 { return r.eof }

func TestReaderSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "size.bin")
	if err := os.WriteFile(path, []byte("0123456789"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open temp file: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := file.Close(); closeErr != nil {
			t.Errorf("close temp file: %v", closeErr)
		}
	})

	closed, err := os.Open(path)
	if err != nil {
		t.Fatalf("open file to close: %v", err)
	}
	if err := closed.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}

	tests := []struct {
		name   string
		r      io.ReaderAt
		want   uint64
		wantOK bool
	}{
		{
			name:   "os file reports its size",
			r:      file,
			want:   10,
			wantOK: true,
		},
		{
			name:   "bytes reader reports its size",
			r:      bytes.NewReader([]byte("hello")),
			want:   5,
			wantOK: true,
		},
		{
			name:   "empty bytes reader",
			r:      bytes.NewReader(nil),
			want:   0,
			wantOK: true,
		},
		{
			name:   "section reader reports its section length",
			r:      io.NewSectionReader(bytes.NewReader(make([]byte, 100)), 10, 20),
			want:   20,
			wantOK: true,
		},
		{
			name:   "empty section reader",
			r:      io.NewSectionReader(bytes.NewReader(nil), 0, 0),
			want:   0,
			wantOK: true,
		},
		{
			name:   "reader without a size method is unknown",
			r:      noSizeReader{},
			want:   0,
			wantOK: false,
		},
		{
			name:   "closed file is unknown",
			r:      closed,
			want:   0,
			wantOK: false,
		},
		{
			name:   "stat error is unknown",
			r:      statErrorReader{},
			want:   0,
			wantOK: false,
		},
		{
			name:   "negative size is unknown",
			r:      negativeSizeReader{},
			want:   0,
			wantOK: false,
		},
		{
			name:   "logical end of file wins over the physical size",
			r:      logicalEOFReader{eof: 4096},
			want:   4096,
			wantOK: true,
		},
		{
			name:   "logical end of file of zero is a known size",
			r:      logicalEOFReader{eof: 0},
			want:   0,
			wantOK: true,
		},
		{
			name:   "nil reader is unknown",
			r:      nil,
			want:   0,
			wantOK: false,
		},
		{
			name:   "typed nil pointer is unknown and must not panic",
			r:      (*bytes.Reader)(nil),
			want:   0,
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ReaderSize(tt.r)
			if ok != tt.wantOK {
				t.Errorf("ReaderSize() ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("ReaderSize() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestValidateRegion(t *testing.T) {
	const what = "test region"

	tests := []struct {
		name        string
		addr        uint64
		size        uint64
		limit       uint64
		wantErr     bool
		errContains []string
	}{
		{
			name:  "region inside the file",
			addr:  100,
			size:  50,
			limit: 1000,
		},
		{
			name:  "region starts at zero",
			addr:  0,
			size:  1000,
			limit: 1000,
		},
		{
			name:  "region ends exactly at the file size",
			addr:  950,
			size:  50,
			limit: 1000,
		},
		{
			name:        "region ends one byte past the file size",
			addr:        951,
			size:        50,
			limit:       1000,
			wantErr:     true,
			errContains: []string{"read beyond end of file", what, "offset=951", "size=50", "fileSize=1000"},
		},
		{
			name:        "addr plus size wraps around",
			addr:        math.MaxUint64,
			size:        2,
			limit:       0,
			wantErr:     true,
			errContains: []string{"address overflow", what, "addr=18446744073709551615", "size=2"},
		},
		{
			name:        "addr max with size one is an overflow",
			addr:        math.MaxUint64,
			size:        1,
			limit:       0,
			wantErr:     true,
			errContains: []string{"address overflow"},
		},
		{
			name:  "largest address range that does not wrap",
			addr:  math.MaxUint64 - 1,
			size:  1,
			limit: 0,
		},
		{
			name:  "zero size with a bogus address is valid",
			addr:  math.MaxUint64,
			size:  0,
			limit: 10,
		},
		{
			name:  "zero size with a huge limit is valid",
			addr:  math.MaxUint64,
			size:  0,
			limit: math.MaxUint64,
		},
		{
			name:  "unknown limit skips the limit check",
			addr:  math.MaxUint64 - 100,
			size:  100,
			limit: 0,
		},
		{
			name:        "unknown limit still checks for wrap",
			addr:        math.MaxUint64 - 1,
			size:        2,
			limit:       0,
			wantErr:     true,
			errContains: []string{"address overflow"},
		},
		{
			name:  "single byte at the start with unknown limit",
			addr:  0,
			size:  1,
			limit: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRegion(tt.addr, tt.size, tt.limit, what)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRegion(%d, %d, %d) error = %v, wantErr %v", tt.addr, tt.size, tt.limit, err, tt.wantErr)
				return
			}

			for _, want := range tt.errContains {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("ValidateRegion(%d, %d, %d) error = %v, want error containing %q", tt.addr, tt.size, tt.limit, err, want)
				}
			}
		})
	}
}
