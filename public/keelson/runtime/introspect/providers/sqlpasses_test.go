package providers

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/keelson/data/passreg"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// TestSqlPassesTableRows builds the table from crafted entries (not the
// process-global passreg.Default), mirroring how envTable is tested.
func TestSqlPassesTableRows(t *testing.T) {
	es := []passreg.Entry{
		{
			Pass: nanopass.LiftBodyPass("ProbePass", func(sql string) (string, error) { return sql, nil },
				nanopass.PassProperties{NeedsFixedPoint: true, Reads: nanopass.RegionBody | nanopass.RegionParams, Writes: nanopass.RegionBody}),
			Stage:       passreg.StagePreExecute,
			Order:       100,
			Description: "probe",
			Provenance:  "example/probe",
		},
	}
	rec := sqlPassesTable(es).Build(introspect.AllColumns(), len(es))
	defer rec.Release()

	require.EqualValues(t, 1, rec.NumRows())
	assert.Equal(t, "pre-execute", firstString(t, rec, "stage"))
	assert.Equal(t, "ProbePass", firstString(t, rec, "name"))
	assert.Equal(t, "example/probe", firstString(t, rec, "provenance"))

	idx := rec.Schema().FieldIndices("needs_fixed_point")
	require.NotEmpty(t, idx)
	assert.True(t, rec.Column(idx[0]).(*array.Boolean).Value(0))
}

func TestRegionNames(t *testing.T) {
	assert.Empty(t, regionNames(0))
	assert.Equal(t, []string{"body"}, regionNames(nanopass.RegionBody))
	assert.Equal(t,
		[]string{"body", "session_settings", "statement_settings", "params", "format"},
		regionNames(nanopass.RegionBody|nanopass.RegionSessionSettings|nanopass.RegionStatementSettings|nanopass.RegionParams|nanopass.RegionFormat))
}
