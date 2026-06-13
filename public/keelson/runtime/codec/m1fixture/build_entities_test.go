package m1fixture

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	cbdml "github.com/stergiotis/boxer/public/keelson/runtime/factsschema/dml_cbor"
)

// TestBuildEntities_Smoke proves the new schema-agnostic
// M1SampleBuildEntities generic function compiles AND works when
// passed the concrete facts DML instance. Go's type inference picks
// up every per-section type parameter from the dml argument alone.
//
// This is the M1 entry point for replacing the facts-locked
// Marshal(w io.Writer) method with the composed-interface helper —
// it demonstrates the same end-state arrow.Record output via a
// schema-free code path.
func TestBuildEntities_Smoke(t *testing.T) {
	orig := sampleM1Sample()
	cols := &M1SampleColumns{}
	cols.Append(orig)
	cols.Append(orig)
	cols.Append(orig)

	dml := cbdml.NewInEntityFacts(memory.NewGoAllocator(), 3)
	dml.SetActiveSections(M1SampleActiveSections)
	dml.Builder().SetActiveFields(M1SampleActiveFields())

	// The call. Go infers all 25 type parameters from dml's concrete
	// method signatures — no explicit instantiation needed.
	err := M1SampleBuildEntities(dml, cols)
	require.NoError(t, err)

	recs, err := dml.TransferRecords(nil)
	require.NoError(t, err)
	require.NotEmpty(t, recs)
	var totalRows int64
	for _, r := range recs {
		totalRows += r.NumRows()
		r.Release()
	}
	require.Equal(t, int64(3), totalRows)
}
