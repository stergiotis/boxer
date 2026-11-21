package queryrunsvc

import (
	"context"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// With an insert transform registered for the destination the view is not
// installed: the view appends on the server, where no transform runs
// (ADR-0402 §SD5). The client points at a closed port: the refusal stands
// whether or not the server could be asked about a leftover view, and no
// setup statement is attempted.
func TestReconcileRefusesUnderATransform(t *testing.T) {
	s, err := New(Config{Database: "scratch_qr", Table: "facts", ChURL: "http://127.0.0.1:1/"}, zerolog.Nop())
	require.NoError(t, err)
	unregister := recordstore.RegisterInsertTransform("scratch_qr.facts", recordstore.InsertTransformFunc(
		func(_ string, records []arrow.RecordBatch) (string, []arrow.RecordBatch, error) {
			return "scratch_qr.carrier", records, nil
		}))
	defer unregister()
	err = s.Reconcile(context.Background())
	require.ErrorIs(t, err, recordstore.ErrTransformedTable)
}
