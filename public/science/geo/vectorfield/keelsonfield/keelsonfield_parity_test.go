package keelsonfield_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/science/geo/vectorfield/keelsonfield/keelsonfieldtest"
)

// The family answers every purpose as sqlfield's reduction statements do
// over the same relation (ADR-0291 §SD5), on a field with masked nodes.
func TestFamilyAnswersAsTheReductionStatementsDo(t *testing.T) {
	keelsonfieldtest.Parity(t, "storm", keelsonfieldtest.Storm(t), keelsonfieldtest.Options{MinMissing: 10})
}
