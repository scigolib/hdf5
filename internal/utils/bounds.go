package utils

import (
	"fmt"
	"io"
	"io/fs"
	"math"
	"reflect"
)

// ReaderSize reports the total size of r when the reader can tell. Returns
// (0, false) when the size is unknown.
//
// This mirrors H5FD_get_eof in the C library: a driver can report the real end
// of file, and every later region check is only as good as that number. Only
// the two shapes the library actually uses are recognized: *os.File via
// Stat(), and *bytes.Reader / io.SectionReader via Size(). A negative size is
// treated as unknown because it cannot describe a byte range.
func ReaderSize(r io.ReaderAt) (uint64, bool) {
	if isNilReader(r) {
		return 0, false
	}

	// Stat first: a driver-backed reader reports the size of the underlying
	// file, which is what region checks must compare against.
	switch s := r.(type) {
	case interface{ Stat() (fs.FileInfo, error) }:
		info, err := s.Stat()
		if err != nil || info == nil {
			return 0, false
		}

		size := info.Size()
		if size < 0 {
			return 0, false
		}

		return uint64(size), true
	case interface{ Size() int64 }:
		size := s.Size()
		if size < 0 {
			return 0, false
		}

		return uint64(size), true
	default:
		return 0, false
	}
}

// isNilReader reports whether r is nil, including a typed nil pointer such as
// (*bytes.Reader)(nil). The latter is a non-nil interface whose method call
// would panic, so the plain r == nil test is not enough.
func isNilReader(r io.ReaderAt) bool {
	if r == nil {
		return true
	}

	v := reflect.ValueOf(r)
	switch v.Kind() { //nolint:exhaustive // Only nil-able kinds need the IsNil call.
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

// ValidateRegion reports whether [addr, addr+size) lies within [0, limit).
// A zero limit means unknown: the limit comparison is skipped, the wrap check
// is not. A zero size is always valid.
//
// This is the Go counterpart of H5FD_REGION_OVERFLOW: metadata taken from the
// file is untrusted, and an unchecked addr+size both wraps (turning a huge
// offset into a tiny one) and lets a caller size an allocation from a corrupt
// number. Rejecting the wrap first is what keeps the addition below safe.
func ValidateRegion(addr, size, limit uint64, what string) error {
	if size == 0 {
		return nil
	}

	// Overflow occurs exactly when addr > MaxUint64-size, for size > 0.
	if addr > math.MaxUint64-size {
		return fmt.Errorf("address overflow: %s: addr=%d size=%d", what, addr, size)
	}

	if limit != 0 && addr+size > limit {
		return fmt.Errorf("%s: read beyond end of file: offset=%d size=%d fileSize=%d", what, addr, size, limit)
	}

	return nil
}
