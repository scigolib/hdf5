package utils

import "fmt"

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
