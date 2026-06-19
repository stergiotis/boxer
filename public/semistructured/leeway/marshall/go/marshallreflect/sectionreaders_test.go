package marshallreflect_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
)

// TestSectionReaders_NilReported rejects a nil reader set up front rather than
// dereferencing it mid-row.
func TestSectionReaders_NilReported(t *testing.T) {
	var got []mixedVerbatimDrone
	require.Error(t, marshallreflect.Unmarshal(nil, &got, nil))
}

// TestSectionReaders_MissingCoverageReported confirms Unmarshal reports every
// plain column and section the DTO's Plan declares but the reader set omits —
// in one error, before reading any row (the win over the old func(name) any
// triplet, where a forgotten section returned nil and panicked at row i).
func TestSectionReaders_MissingCoverageReported(t *testing.T) {
	var got []mixedVerbatimDrone // declares id + naturalKey plain + the symbol section
	// Register nothing.
	err := marshallreflect.Unmarshal(marshallreflect.NewSectionReaders(0), &got, marshallreflect.NoLookup{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "id")
	require.Contains(t, err.Error(), "symbol")
}
