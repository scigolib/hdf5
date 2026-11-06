package structures

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSymbolTableSignature(t *testing.T) {
	// Verify the constant
	require.Equal(t, "SNOD", SymbolTableSignature)
}
