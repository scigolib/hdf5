package utils

import (
	"math"
	"strings"
	"testing"
)

// TestSafeInt pins the contract of the file-metadata size conversion guard.
//
// The interesting boundary is math.MaxInt, which is 2^63-1 on a 64-bit host and
// 2^31-1 on a 32-bit target. The test is written against math.MaxInt rather than
// MaxInt32 so the same table passes in both widths; the 32-bit half of the
// contract is executed by GOARCH=386 go vet/build (the host is amd64).
func TestSafeInt(t *testing.T) {
	tests := []struct {
		name    string
		size    uint64
		want    int
		wantErr bool
	}{
		{name: "zero", size: 0, want: 0, wantErr: false},
		{name: "small size", size: 4096, want: 4096, wantErr: false},
		{name: "platform maximum", size: uint64(math.MaxInt), want: math.MaxInt, wantErr: false},
		{name: "one above platform maximum", size: uint64(math.MaxInt) + 1, wantErr: true},
		{name: "max uint64", size: math.MaxUint64, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SafeInt(tt.size, "test size")

			if tt.wantErr {
				if err == nil {
					t.Fatalf("SafeInt(%d) = %d, want error", tt.size, got)
				}
				if !strings.Contains(err.Error(), "test size") {
					t.Errorf("error %q does not name the value being converted", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("SafeInt(%d) returned unexpected error: %v", tt.size, err)
			}
			if got != tt.want {
				t.Errorf("SafeInt(%d) = %d, want %d", tt.size, got, tt.want)
			}
		})
	}
}
