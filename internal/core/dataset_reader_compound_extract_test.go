package core

import "testing"

func TestExtractString(t *testing.T) {
	tests := []struct {
		name        string
		data        []byte
		paddingType uint8
		want        string
	}{
		// Null-terminated (padding type 0)
		{
			name:        "null-terminated: middle null",
			data:        []byte{'H', 'e', 'l', 'l', 'o', 0x00, 'X', 'X'},
			paddingType: 0,
			want:        "Hello",
		},
		{
			name:        "null-terminated: no null",
			data:        []byte{'H', 'e', 'l', 'l', 'o'},
			paddingType: 0,
			want:        "Hello",
		},
		{
			name:        "null-terminated: first byte null",
			data:        []byte{0x00, 'H', 'e', 'l', 'l', 'o'},
			paddingType: 0,
			want:        "",
		},
		{
			name:        "null-terminated: empty",
			data:        []byte{},
			paddingType: 0,
			want:        "",
		},
		{
			name:        "null-terminated: all nulls",
			data:        []byte{0x00, 0x00, 0x00},
			paddingType: 0,
			want:        "",
		},

		// Null-padded (padding type 1)
		{
			name:        "null-padded: trailing nulls",
			data:        []byte{'H', 'e', 'l', 'l', 'o', 0x00, 0x00, 0x00},
			paddingType: 1,
			want:        "Hello",
		},
		{
			name:        "null-padded: no trailing nulls",
			data:        []byte{'H', 'e', 'l', 'l', 'o'},
			paddingType: 1,
			want:        "Hello",
		},
		{
			name:        "null-padded: middle null preserved",
			data:        []byte{'H', 'i', 0x00, 'B', 'y', 'e', 0x00},
			paddingType: 1,
			want:        "Hi\x00Bye",
		},
		{
			name:        "null-padded: all nulls",
			data:        []byte{0x00, 0x00, 0x00},
			paddingType: 1,
			want:        "",
		},
		{
			name:        "null-padded: empty",
			data:        []byte{},
			paddingType: 1,
			want:        "",
		},
		{
			name:        "null-padded: single null",
			data:        []byte{0x00},
			paddingType: 1,
			want:        "",
		},

		// Space-padded (padding type 2)
		{
			name:        "space-padded: trailing spaces",
			data:        []byte{'H', 'e', 'l', 'l', 'o', ' ', ' ', ' '},
			paddingType: 2,
			want:        "Hello",
		},
		{
			name:        "space-padded: no trailing spaces",
			data:        []byte{'H', 'e', 'l', 'l', 'o'},
			paddingType: 2,
			want:        "Hello",
		},
		{
			name:        "space-padded: middle space preserved",
			data:        []byte{'H', 'i', ' ', 'B', 'y', 'e', ' ', ' '},
			paddingType: 2,
			want:        "Hi Bye",
		},
		{
			name:        "space-padded: trailing nulls and spaces",
			data:        []byte{'H', 'e', 'l', 'l', 'o', ' ', 0x00, ' '},
			paddingType: 2,
			want:        "Hello",
		},
		{
			name:        "space-padded: all spaces",
			data:        []byte{' ', ' ', ' '},
			paddingType: 2,
			want:        "",
		},
		{
			name:        "space-padded: empty",
			data:        []byte{},
			paddingType: 2,
			want:        "",
		},
		{
			name:        "space-padded: mixed trailing",
			data:        []byte{'T', 'e', 's', 't', ' ', 0x00, 0x00, ' '},
			paddingType: 2,
			want:        "Test",
		},

		// Unknown padding type (default case)
		{
			name:        "unknown padding: return as-is",
			data:        []byte{'H', 'e', 'l', 'l', 'o', 0x00, ' ', 'X'},
			paddingType: 99,
			want:        "Hello\x00 X",
		},
		{
			name:        "unknown padding: empty",
			data:        []byte{},
			paddingType: 255,
			want:        "",
		},

		// UTF-8 strings
		{
			name:        "null-terminated: UTF-8",
			data:        append([]byte("Привет"), 0x00, 0x00),
			paddingType: 0,
			want:        "Привет",
		},
		{
			name:        "null-padded: UTF-8",
			data:        append([]byte("日本語"), 0x00, 0x00),
			paddingType: 1,
			want:        "日本語",
		},
		{
			name:        "space-padded: UTF-8",
			data:        append([]byte("こんにちは"), ' ', ' '),
			paddingType: 2,
			want:        "こんにちは",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractString(tt.data, tt.paddingType)
			if got != tt.want {
				t.Errorf("extractString() = %q, want %q", got, tt.want)
			}
		})
	}
}
