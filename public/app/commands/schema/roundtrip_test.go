package schema

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

func TestPrintRoundTrip(t *testing.T) {
	ok := chstore.RoundTripReport{Kinds: []string{"a", "b"}, Written: 2, Read: 2, Target: "carrier.rows", ReadTable: "carrier.facts_view",
		Columns: recordstore.SchemaVerdict{Table: "carrier.facts_view", Verdict: recordstore.SchemaVerdictOk}}
	var b bytes.Buffer
	PrintRoundTrip(&b, ok)
	assert.Contains(t, b.String(), "written to: carrier.rows; read through: carrier.facts_view")
	assert.Contains(t, b.String(), "ok: every row and every column survived")

	failed := ok
	failed.Mismatches = []chstore.RoundTripMismatch{{Column: "ts", Rows: 4}}
	b.Reset()
	PrintRoundTrip(&b, failed)
	assert.Contains(t, b.String(), "differs: ts (4 rows")
	assert.NotContains(t, b.String(), "ok:")
}
