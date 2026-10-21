package utils

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSafeAdd covers the addition guard used where two file-derived sizes are
// summed before an allocation or a read.
func TestSafeAdd(t *testing.T) {
	tests := []struct {
		name    string
		a, b    uint64
		want    uint64
		wantErr bool
	}{
		{name: "zero plus zero", a: 0, b: 0, want: 0},
		{name: "small values", a: 5, b: 7, want: 12},
		{name: "exact max", a: math.MaxUint64 - 1, b: 1, want: math.MaxUint64},
		{name: "overflow by one", a: math.MaxUint64, b: 1, wantErr: true},
		{name: "both max", a: math.MaxUint64, b: math.MaxUint64, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SafeAdd(tt.a, tt.b)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
