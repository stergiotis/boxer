package hostboot

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillpresence"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// The host checks the facts table once, by the columns chstore creates it
// with; that covers the facts-bound stores only while their generated
// columns are those columns. A store regenerated against another facts
// shape fails here rather than unchecked at run time.
func TestFactsBoundStoresShareTheFactsColumns(t *testing.T) {
	facts, err := chstore.SchemaContract(chstore.Defaults())
	require.NoError(t, err)
	live := make([]recordstore.LiveColumn, 0, len(facts.Columns))
	for _, c := range facts.Columns {
		live = append(live, recordstore.LiveColumn{Name: c.Name, Type: c.Type})
	}
	for _, c := range []recordstore.TableContract{trail.TrailSchemaContract(""), watchbillpresence.PresenceSchemaContract("")} {
		v := recordstore.JudgeColumns(c.Columns, live)
		require.Equal(t, recordstore.SchemaVerdictOk, v.Verdict, c.Kind+": "+v.Summary())
		require.Empty(t, v.Extra, c.Kind)
		require.Equal(t, facts.Table, c.Table)
	}
}
