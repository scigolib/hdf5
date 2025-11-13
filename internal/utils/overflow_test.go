package utils

import (
	"math"
	"strings"
	"testing"
)

func TestCheckMultiplyOverflow(t *testing.T) {
	tests := []struct {
		name    string
		a       uint64
		b       uint64
		wantErr bool
	}{
		{
			name:    "no overflow - small numbers",
			a:       10,
			b:       20,
			wantErr: false,
		},
		{
			name:    "no overflow - one zero",
			a:       0,
			b:       math.MaxUint64,
			wantErr: false,
		},
		{
			name:    "no overflow - both zero",
			a:       0,
			b:       0,
			wantErr: false,
		},
		{
			name:    "overflow - max * 2",
			a:       math.MaxUint64,
			b:       2,
			wantErr: true,
		},
		{
			name:    "overflow - large numbers",
			a:       math.MaxUint64 / 2,
			b:       3,
			wantErr: true,
		},
		{
			name:    "no overflow - exact max",
			a:       math.MaxUint64,
			b:       1,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckMultiplyOverflow(tt.a, tt.b)
			if (err != nil) != tt.wantErr {
				t.Errorf("CheckMultiplyOverflow(%d, %d) error = %v, wantErr %v", tt.a, tt.b, err, tt.wantErr)
			}
		})
	}
}

func TestSafeMultiply(t *testing.T) {
	tests := []struct {
		name    string
		a       uint64
		b       uint64
		want    uint64
		wantErr bool
	}{
		{
			name:    "normal multiplication",
			a:       10,
			b:       20,
			want:    200,
			wantErr: false,
		},
		{
			name:    "zero multiplication",
			a:       0,
			b:       100,
			want:    0,
			wantErr: false,
		},
		{
			name:    "overflow",
			a:       math.MaxUint64,
			b:       2,
			want:    0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SafeMultiply(tt.a, tt.b)
			if (err != nil) != tt.wantErr {
				t.Errorf("SafeMultiply(%d, %d) error = %v, wantErr %v", tt.a, tt.b, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("SafeMultiply(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestCalculateChunkSize(t *testing.T) {
	tests := []struct {
		name        string
		dimensions  []uint32
		elementSize uint64
		want        uint64
		wantErr     bool
		errContains string
	}{
		{
			name:        "normal chunk",
			dimensions:  []uint32{10, 20, 30},
			elementSize: 8,
			want:        10 * 20 * 30 * 8,
			wantErr:     false,
		},
		{
			name:        "1D chunk",
			dimensions:  []uint32{1000},
			elementSize: 4,
			want:        4000,
			wantErr:     false,
		},
		{
			name:        "no dimensions",
			dimensions:  []uint32{},
			elementSize: 8,
			want:        0,
			wantErr:     true,
			errContains: "no dimensions",
		},
		{
			name:        "zero element size",
			dimensions:  []uint32{10, 20},
			elementSize: 0,
			want:        0,
			wantErr:     true,
			errContains: "element size cannot be zero",
		},
		{
			name:        "dimension overflow",
			dimensions:  []uint32{math.MaxUint32, math.MaxUint32},
			elementSize: 8,
			want:        0,
			wantErr:     true,
			errContains: "overflow",
		},
		{
			name:        "element size overflow",
			dimensions:  []uint32{math.MaxUint32, 2},
			elementSize: math.MaxUint64 / 2,
			want:        0,
			wantErr:     true,
			errContains: "overflow",
		},
		{
			name:        "CVE-2025-7067 test case - huge chunks",
			dimensions:  []uint32{4294967295, 4294967295}, // MaxUint32 x MaxUint32
			elementSize: 8,
			want:        0,
			wantErr:     true,
			errContains: "overflow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CalculateChunkSize(tt.dimensions, tt.elementSize)
			if (err != nil) != tt.wantErr {
				t.Errorf("CalculateChunkSize(%v, %d) error = %v, wantErr %v", tt.dimensions, tt.elementSize, err, tt.wantErr)
				return
			}
			if err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("CalculateChunkSize(%v, %d) error = %v, want error containing %q", tt.dimensions, tt.elementSize, err, tt.errContains)
				}
			}
			if got != tt.want {
				t.Errorf("CalculateChunkSize(%v, %d) = %d, want %d", tt.dimensions, tt.elementSize, got, tt.want)
			}
		})
	}
}

func TestCalculateChunkSize64(t *testing.T) {
	tests := []struct {
		name        string
		dimensions  []uint64
		elementSize uint64
		want        uint64
		wantErr     bool
		errContains string
	}{
		{
			name:        "normal 64-bit chunk",
			dimensions:  []uint64{10, 20, 30},
			elementSize: 8,
			want:        10 * 20 * 30 * 8,
			wantErr:     false,
		},
		{
			name:        "large 64-bit chunk (>4GB)",
			dimensions:  []uint64{4294967296, 2}, // 4GB x 2
			elementSize: 1,
			want:        8589934592, // 8GB
			wantErr:     false,
		},
		{
			name:        "overflow 64-bit",
			dimensions:  []uint64{math.MaxUint64, 2},
			elementSize: 1,
			want:        0,
			wantErr:     true,
			errContains: "overflow",
		},
		{
			name:        "no dimensions",
			dimensions:  []uint64{},
			elementSize: 8,
			want:        0,
			wantErr:     true,
			errContains: "no dimensions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CalculateChunkSize64(tt.dimensions, tt.elementSize)
			if (err != nil) != tt.wantErr {
				t.Errorf("CalculateChunkSize64(%v, %d) error = %v, wantErr %v", tt.dimensions, tt.elementSize, err, tt.wantErr)
				return
			}
			if err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("CalculateChunkSize64(%v, %d) error = %v, want error containing %q", tt.dimensions, tt.elementSize, err, tt.errContains)
				}
			}
			if got != tt.want {
				t.Errorf("CalculateChunkSize64(%v, %d) = %d, want %d", tt.dimensions, tt.elementSize, got, tt.want)
			}
		})
	}
}

func TestValidateBufferSize(t *testing.T) {
	tests := []struct {
		name        string
		size        uint64
		maxSize     uint64
		description string
		wantErr     bool
		errContains string
	}{
		{
			name:        "valid size",
			size:        1000,
			maxSize:     10000,
			description: "test buffer",
			wantErr:     false,
		},
		{
			name:        "exact max",
			size:        10000,
			maxSize:     10000,
			description: "test buffer",
			wantErr:     false,
		},
		{
			name:        "zero size",
			size:        0,
			maxSize:     10000,
			description: "test buffer",
			wantErr:     true,
			errContains: "cannot be zero",
		},
		{
			name:        "exceeds max",
			size:        10001,
			maxSize:     10000,
			description: "test buffer",
			wantErr:     true,
			errContains: "exceeds maximum",
		},
		{
			name:        "CVE-2025-6269 test - huge attribute",
			size:        100 * 1024 * 1024, // 100MB
			maxSize:     MaxAttributeSize,
			description: "attribute",
			wantErr:     true,
			errContains: "exceeds maximum",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateBufferSize(tt.size, tt.maxSize, tt.description)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateBufferSize(%d, %d, %q) error = %v, wantErr %v", tt.size, tt.maxSize, tt.description, err, tt.wantErr)
				return
			}
			if err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("ValidateBufferSize(%d, %d, %q) error = %v, want error containing %q", tt.size, tt.maxSize, tt.description, err, tt.errContains)
				}
			}
		})
	}
}
