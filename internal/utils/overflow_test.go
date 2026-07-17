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
