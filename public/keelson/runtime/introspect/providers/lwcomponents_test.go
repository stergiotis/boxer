package providers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/componentsql"
)

// withRegisteredComponents populates the process registry for the duration of
// a test and puts it back. The provider reads componentsql.Default by design —
// the table answers "what can I query in THIS process" — so a test that wants
// rows has to register some.
func withRegisteredComponents(t *testing.T, sets ...componentsql.Set) {
	t.Helper()
	componentsql.Default.Reset()
	t.Cleanup(componentsql.Default.Reset)
	for _, s := range sets {
		require.NoError(t, componentsql.Default.Register(s))
	}
}

func probeSet() componentsql.Set {
	return componentsql.Set{
		Store: "Sysmetrics",
		Table: "boxer.facts",
		Kinds: map[string]componentsql.Artefacts{
			"SysMem": {
				Presence: "has(lr, 1)", Validator: "countEqual(lr, 1) = 1",
				Filter: "has(lr, 1) AND countEqual(lr, 1) = 1", Projection: "CAST(tuple(x), 'Tuple(X UInt64)')",
			},
			"SysCpu": {
				Presence: "has(lr, 2)", Validator: "countEqual(lr, 2) = 1",
				Filter: "has(lr, 2) AND countEqual(lr, 2) = 1", Projection: "CAST(tuple(y), 'Tuple(Y UInt8)')",
			},
		},
	}
}

// The table publishes what a call needs: the kind to name, and the table the
// read must be written against — the artefacts carry unqualified columns, so
// the FROM is not a detail a reader can infer.
func TestLwComponentsPublishesKindAndBoundTable(t *testing.T) {
	withRegisteredComponents(t, probeSet())

	rows := lwComponentRows()
	require.Len(t, rows, 2)
	assert.Equal(t, "SysCpu", rows[0].kind, "rows are sorted by kind, so the table is a stable diff target")
	assert.Equal(t, "SysMem", rows[1].kind)

	for _, r := range rows {
		assert.Equal(t, "boxer.facts", r.table)
		assert.Equal(t, "Sysmetrics", r.store)
		assert.Positive(t, r.filter, "a kind with no filter should not be registrable")
		assert.Positive(t, r.projection, "a kind with no projection should not be registrable")
	}
}

// The rows are the registry's, not the link set's. A process that links a
// store but never registers it cannot query that store's components, and the
// table has to say so — promising otherwise is worse than an empty table.
func TestLwComponentsReflectsRegistrationNotLinkage(t *testing.T) {
	withRegisteredComponents(t)
	assert.Empty(t, lwComponentRows(),
		"an unwired process publishes no components, however many stores it links")

	require.NoError(t, componentsql.Default.Register(probeSet()))
	assert.Len(t, lwComponentRows(), 2, "registration is what makes a kind appear")
}

// Freshness is Live because registration happens at wiring time but nothing
// forces it to: a cached empty snapshot would make a late registration look
// like an absent store.
func TestLwComponentsIsLive(t *testing.T) {
	assert.Equal(t, "live", lwComponentsProvider{}.Freshness().String())
}

// The schema is fixed regardless of what is registered, so an empty process
// still answers `DESCRIBE` and `SELECT *` with the real column set.
func TestLwComponentsSchemaIsIndependentOfContent(t *testing.T) {
	withRegisteredComponents(t)
	empty := lwComponentsProvider{}.Schema()

	withRegisteredComponents(t, probeSet())
	assert.Equal(t, empty.String(), lwComponentsProvider{}.Schema().String())

	names := make([]string, 0, len(empty.Fields()))
	for _, f := range empty.Fields() {
		names = append(names, f.Name)
	}
	assert.Equal(t, []string{"kind", "store", "table",
		"presence_bytes", "validator_bytes", "filter_bytes", "projection_bytes"}, names)
}
