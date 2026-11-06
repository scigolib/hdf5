package utils

import (
	"errors"
	"fmt"
)

// ErrCorrupt marks file metadata that contradicts the file or the HDF5 format:
// a size that overflows, a region past the end of the file, a stored value the
// C library would reject. Wrap it with %w so callers can match it with
// errors.Is; the message must still name the structure and its address.
//
// The root package re-exports it as hdf5.ErrCorrupt. It lives here because
// internal/core cannot import the root package.
var ErrCorrupt = errors.New("hdf5: corrupt file")

// ErrLimit marks a read refused because it would allocate more than the
// configured read limit. Unlike ErrCorrupt the file may be valid: raising the
// limit lets the read proceed. Re-exported as hdf5.ErrLimit.
var ErrLimit = errors.New("hdf5: read limit exceeded")

// ErrNotSupported marks valid HDF5 that this library does not read or write
// yet: a filter without a decoder (N-bit, SZIP, a third-party plugin), a
// message shared through the file-wide SOHM heap. Unlike ErrCorrupt the file
// is fine; unlike ErrLimit no setting changes the outcome. Re-exported as
// hdf5.ErrNotSupported.
var ErrNotSupported = errors.New("hdf5: not supported")

// H5Error represents a structured HDF5 error.
type H5Error struct {
	Context string
	Cause   error
}

// Error implements the error interface.
func (e *H5Error) Error() string {
	return fmt.Sprintf("%s: %v", e.Context, e.Cause)
}

// WrapError creates a contextual error.
func WrapError(context string, cause error) error {
	if cause == nil {
		return nil
	}
	return &H5Error{
		Context: context,
		Cause:   cause,
	}
}

// Unwrap provides compatibility with errors.Unwrap().
func (e *H5Error) Unwrap() error {
	return e.Cause
}
