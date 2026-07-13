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

// TestSqlPassesTableRows builds the table from crafted catalog rows (not the
// process-global passreg.Default), mirroring how envTable is tested. It covers
// both a concrete entry and a late-bound factory descriptor (ADR-0108 §SD7).
func TestSqlPassesTableRows(t *testing.T) {
	rows := []passreg.CatalogRow{
		{
			Stage:       passreg.StagePreExecute,
			Name:        "ProbePass",
			Order:       100,
			Description: "probe",
			Provenance:  "example/probe",
			Properties:  nanopass.PassProperties{NeedsFixedPoint: true, Reads: nanopass.RegionBody | nanopass.RegionParams, Writes: nanopass.RegionBody},
			LateBound:   false,
		},
		{
			Stage:       passreg.StagePreExecute,
			Name:        "LateProbe",
			Order:       200,
			Description: "late-bound probe",
			Provenance:  "example/late",
			Properties:  nanopass.PassProperties{Idempotent: true, Reads: nanopass.RegionBody, Writes: nanopass.RegionBody},
			LateBound:   true,
		},
	}
	rec := sqlPassesTable(rows).Build(introspect.AllColumns(), len(rows))
	defer rec.Release()

	require.EqualValues(t, 2, rec.NumRows())
	assert.Equal(t, "pre-execute", firstString(t, rec, "stage"))
	assert.Equal(t, "ProbePass", firstString(t, rec, "name"))
	assert.Equal(t, "example/probe", firstString(t, rec, "provenance"))

	nfpIdx := rec.Schema().FieldIndices("needs_fixed_point")
	require.NotEmpty(t, nfpIdx)
	assert.True(t, rec.Column(nfpIdx[0]).(*array.Boolean).Value(0))

	lbIdx := rec.Schema().FieldIndices("late_bound")
	require.NotEmpty(t, lbIdx)
	lb := rec.Column(lbIdx[0]).(*array.Boolean)
	assert.False(t, lb.Value(0), "concrete entry row must not be late_bound")
	assert.True(t, lb.Value(1), "factory row must be late_bound")
}

func TestRegionNames(t *testing.T) {
	assert.Empty(t, regionNames(0))
	assert.Equal(t, []string{"body"}, regionNames(nanopass.RegionBody))
	assert.Equal(t,
		[]string{"body", "session_settings", "statement_settings", "params", "format"},
		regionNames(nanopass.RegionBody|nanopass.RegionSessionSettings|nanopass.RegionStatementSettings|nanopass.RegionParams|nanopass.RegionFormat))
}
