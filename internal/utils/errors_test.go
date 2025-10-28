package utils

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestH5Error_Error(t *testing.T) {
	tests := []struct {
		name     string
		context  string
		cause    error
		expected string
	}{
		{
			name:     "simple error",
			context:  "reading superblock",
			cause:    errors.New("invalid signature"),
			expected: "reading superblock: invalid signature",
		},
		{
			name:     "nested error",
			context:  "parsing dataset",
			cause:    errors.New("dimension mismatch"),
			expected: "parsing dataset: dimension mismatch",
		},
		{
			name:     "empty context",
			context:  "",
			cause:    errors.New("some error"),
			expected: ": some error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &H5Error{
				Context: tt.context,
				Cause:   tt.cause,
			}
			require.Equal(t, tt.expected, err.Error())
		})
	}
}

func TestWrapError(t *testing.T) {
	tests := []struct {
		name    string
		context string
		cause   error
		wantNil bool
	}{
		{
			name:    "wrap non-nil error",
			context: "reading data",
			cause:   errors.New("IO error"),
			wantNil: false,
		},
		{
			name:    "wrap nil error returns nil",
			context: "some operation",
			cause:   nil,
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := WrapError(tt.context, tt.cause)

			if tt.wantNil {
				require.Nil(t, err)
				return
			}

			require.NotNil(t, err)

			// Verify it's an H5Error
			var h5err *H5Error
			ok := errors.As(err, &h5err)
			require.True(t, ok, "error should be H5Error type")
			require.Equal(t, tt.context, h5err.Context)
			require.Equal(t, tt.cause, h5err.Cause)
		})
	}
}

func TestH5Error_Unwrap(t *testing.T) {
	originalErr := errors.New("original error")
	wrapped := WrapError("context", originalErr)

	require.NotNil(t, wrapped)

	// Test using errors.Unwrap
	unwrapped := errors.Unwrap(wrapped)
	require.Equal(t, originalErr, unwrapped)
}

func TestH5Error_ErrorsIs(t *testing.T) {
	originalErr := errors.New("specific error")
	wrapped := WrapError("first level", originalErr)
	doubleWrapped := WrapError("second level", wrapped)

	// errors.Is should work through the chain
	require.True(t, errors.Is(doubleWrapped, originalErr))
	require.True(t, errors.Is(wrapped, originalErr))
}

func TestH5Error_ErrorsAs(t *testing.T) {
	originalErr := errors.New("base error")
	wrapped := WrapError("context", originalErr)

	var h5err *H5Error
	require.True(t, errors.As(wrapped, &h5err))
	require.Equal(t, "context", h5err.Context)
	require.Equal(t, originalErr, h5err.Cause)
}

func TestWrapError_ChainedWrapping(t *testing.T) {
	// Test multiple levels of wrapping
	baseErr := errors.New("base error")
	level1 := WrapError("level 1", baseErr)
	level2 := WrapError("level 2", level1)
	level3 := WrapError("level 3", level2)

	require.NotNil(t, level3)

	// Verify error message contains all contexts
	errMsg := level3.Error()
	require.Contains(t, errMsg, "level 3")
	require.Contains(t, errMsg, "level 2")

	// Verify unwrapping works
	require.True(t, errors.Is(level3, baseErr))

	// Verify we can extract each level
	var h5err *H5Error

	require.True(t, errors.As(level3, &h5err))
	require.Equal(t, "level 3", h5err.Context)

	// Unwrap once
	unwrapped1 := errors.Unwrap(level3)
	require.True(t, errors.As(unwrapped1, &h5err))
	require.Equal(t, "level 2", h5err.Context)

	// Unwrap again
	unwrapped2 := errors.Unwrap(unwrapped1)
	require.True(t, errors.As(unwrapped2, &h5err))
	require.Equal(t, "level 1", h5err.Context)

	// Final unwrap gets base error
	unwrapped3 := errors.Unwrap(unwrapped2)
	require.Equal(t, baseErr, unwrapped3)
}

func TestWrapError_RealWorldScenarios(t *testing.T) {
	t.Run("file reading error", func(t *testing.T) {
		ioErr := errors.New("unexpected EOF")
		err := WrapError("reading superblock", ioErr)

		require.NotNil(t, err)
		require.Contains(t, err.Error(), "reading superblock")
		require.Contains(t, err.Error(), "unexpected EOF")
		require.True(t, errors.Is(err, ioErr))
	})

	t.Run("parsing error chain", func(t *testing.T) {
		parseErr := errors.New("invalid format")
		datasetErr := WrapError("parsing dataset", parseErr)
		groupErr := WrapError("reading group", datasetErr)
		fileErr := WrapError("opening file", groupErr)

		require.NotNil(t, fileErr)

		// Should be able to find original error
		require.True(t, errors.Is(fileErr, parseErr))

		// Error message should be descriptive
		msg := fileErr.Error()
		require.Contains(t, msg, "opening file")
	})

	t.Run("nil error in chain", func(t *testing.T) {
		var baseErr error
		wrapped := WrapError("some context", baseErr)

		require.Nil(t, wrapped, "wrapping nil should return nil")
	})
}

func TestH5Error_StructFields(t *testing.T) {
	ctx := "test context"
	cause := errors.New("test cause")

	err := &H5Error{
		Context: ctx,
		Cause:   cause,
	}

	// Verify fields are accessible
	require.Equal(t, ctx, err.Context)
	require.Equal(t, cause, err.Cause)
}

func BenchmarkWrapError(b *testing.B) {
	baseErr := errors.New("base error")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = WrapError("context", baseErr)
	}
}

func BenchmarkWrapErrorNil(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = WrapError("context", nil)
	}
}

func BenchmarkErrorMessage(b *testing.B) {
	err := WrapError("reading superblock",
		WrapError("parsing header",
			errors.New("invalid signature")))

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = err.Error()
	}
}
