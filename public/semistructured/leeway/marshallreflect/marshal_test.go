package marshallreflect_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/marshallreflect"
)

// TestMarshal_Batch drives the []T batch entry point (distinct from the
// RowComposer path) and confirms the cached section grouping is applied
// to every row — i.e. hoisting ComputeGroups out of the per-row loop did
// not drop any row's section emit. Reuses the recordingDML / fakeLookup /
// stackedA fixtures defined in stack_test.go (same test package).
func TestMarshal_Batch(t *testing.T) {
	dml := &recordingDML{}
	rows := []stackedA{{Id: 1, Color: "red"}, {Id: 2, Color: "blue"}}
	require.NoError(t, marshallreflect.Marshal(dml, rows, fakeLookup{}))

	joined := strings.Join(dml.log, "\n")
	require.Equal(t, 2, strings.Count(joined, "BeginEntity"), "one entity frame per row")
	require.Equal(t, 2, strings.Count(joined, "CommitEntity"))
	require.Equal(t, 2, strings.Count(joined, "GetSectionSymbol"), "cached groups applied to every row")
	require.Contains(t, joined, `Symbol.BeginAttribute("red")`)
	require.Contains(t, joined, `Symbol.BeginAttribute("blue")`)
	require.Contains(t, joined, "SetId(1,")
	require.Contains(t, joined, "SetId(2,")
}
