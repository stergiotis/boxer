package provenance

import (
	"testing"

	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stretchr/testify/require"
)

// TestStampersRefusedOnDescriptorStore pins the generated lane-hygiene guard
// (ADR-0112 SD2): the provenance descriptor schema deliberately carries no
// HighCardRef membership column, so configuring Stampers on the descriptor
// store — the self-interning recursion the seam's doc warns against — panics
// at construction instead of silently stamping nothing.
func TestStampersRefusedOnDescriptorStore(t *testing.T) {
	require.Panics(t, func() {
		NewProvenanceStore(nil, nil, ProvenanceStoreConfig{
			Stampers: []recordstore.ReferenceStamper{nil},
		})
	})
}
