package core

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCompoundTypeString tests CompoundType String() method.
func TestCompoundTypeString(t *testing.T) {
	tests := []struct {
		name string
		ct   *CompoundType
		want string
	}{
		{
			name: "simple compound with two fields",
			ct: &CompoundType{
				Size: 12,
				Members: []CompoundMember{
					{
						Name:   "x",
						Offset: 0,
						Type: &DatatypeMessage{
							Class: DatatypeFixed,
							Size:  4,
						},
					},
					{
						Name:   "y",
						Offset: 4,
						Type: &DatatypeMessage{
							Class: DatatypeFloat,
							Size:  8,
						},
					},
				},
			},
			want: "compound{size=12, members=[x:integer (size=4 bytes)@0, y:float (size=8 bytes)@4]}",
		},
		{
			name: "single field compound",
			ct: &CompoundType{
				Size: 8,
				Members: []CompoundMember{
					{
						Name:   "value",
						Offset: 0,
						Type: &DatatypeMessage{
							Class: DatatypeFloat,
							Size:  8,
						},
					},
				},
			},
			want: "compound{size=8, members=[value:float (size=8 bytes)@0]}",
		},
		{
			name: "empty compound",
			ct: &CompoundType{
				Size:    0,
				Members: []CompoundMember{},
			},
			want: "compound{size=0, members=[]}",
		},
		{
			name: "compound with three mixed types",
			ct: &CompoundType{
				Size: 20,
				Members: []CompoundMember{
					{
						Name:   "id",
						Offset: 0,
						Type: &DatatypeMessage{
							Class: DatatypeFixed,
							Size:  4,
						},
					},
					{
						Name:   "name",
						Offset: 4,
						Type: &DatatypeMessage{
							Class: DatatypeString,
							Size:  10,
						},
					},
					{
						Name:   "score",
						Offset: 14,
						Type: &DatatypeMessage{
							Class: DatatypeFloat,
							Size:  4,
						},
					},
				},
			},
			want: "compound{size=20, members=[id:integer (size=4 bytes)@0, name:string (size=10 bytes)@4, score:float (size=4 bytes)@14]}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.ct.String()
			require.Equal(t, tt.want, got)
		})
	}
}
