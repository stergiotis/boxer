//go:build integration

package chstore_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/stergiotis/boxer/public/fs/lading/ladingpolicy"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore/chstoretest"
	_ "github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts"
	_ "github.com/stergiotis/boxer/public/keelson/runtime/trail"
	_ "github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillpresence"
	"github.com/stergiotis/boxer/public/semistructured/leeway/factsschema"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/factsschema/meshdemo"
	_ "github.com/stergiotis/boxer/public/semistructured/markdown/mddocfacts"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
	_ "github.com/stergiotis/boxer/public/streaming/stevedore/stevedorefacts"
)

// With the stores over the facts table linked, the round trip carries the
// rows each of them writes — one entity per component, under the store's
// own memberships — through the transform and back, beside the rows of the
// runtime kinds (ADR-0402 §SD6).
func TestRoundTrip_CarriesTheGeneratedStoresRows_Local(t *testing.T) {
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse local unavailable: %v", err)
	}
	ctx := context.Background()
	contract, err := chstore.SchemaContract(chstore.Config{Database: factsschema.DatabaseName, Table: factsschema.TableName})
	require.NoError(t, err)
	carrier := chstoretest.NewCarrier(contract.Columns, "carrier.rows", "carrier.facts_view", "one")
	for _, stmt := range append(carrier.SetupStatements(), carrier.ViewStatement(nil, nil)) {
		require.NoError(t, exec.Exec(ctx, stmt))
	}

	var names []string
	for _, w := range recordstore.SampleRowWriters(factsschema.QualifiedTableName) {
		names = append(names, w.Name)
	}
	require.Equal(t, []string{"ladingpolicy", "mddocfacts", "meshdemo", "stevedorefacts", "sysmfacts", "trail", "watchbillpresence"}, names,
		"every generated store over the facts table registers its sample rows")

	report, err := chstore.RoundTrip(ctx, exec, chstore.RoundTripOptions{Transform: carrier.Transform(), ReadTable: "carrier.facts_view"})
	require.NoError(t, err)
	assert.True(t, report.Ok(), "%+v", report)
	assert.Equal(t, names, report.Stores)
	assert.EqualValues(t, 48, report.StoreRows, "one entity per component of the seven stores")
	assert.Equal(t, uint64(len(report.Kinds))+report.StoreRows, report.Written)
	assert.Equal(t, report.Written, report.Read)
}
