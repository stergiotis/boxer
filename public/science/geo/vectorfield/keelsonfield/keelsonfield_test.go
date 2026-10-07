package keelsonfield

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/trivialsql"
	"github.com/stergiotis/boxer/public/science/geo/vectorfield"
)

func TestRegister_TheFamilyAndItsOptions(t *testing.T) {
	reg := introspect.NewRegistry()
	require.NoError(t, Register(reg, "storm", stormField(t)))
	for _, s := range []string{"", SuffixOpts, SuffixSteps, SuffixGeometry, SuffixRegularity, SuffixWindow, SuffixSummary} {
		_, ok := reg.Lookup("storm" + s)
		assert.True(t, ok, "storm"+s)
	}
	body, err := trivialsql.Run(context.Background(), reg, "SELECT * FROM keelson('storm_opts')", nil)
	require.NoError(t, err)
	assert.Equal(t, "storm\tm/s\t30\tstorm\n", string(body))

	body, err = trivialsql.Run(context.Background(), reg, "SELECT * FROM keelson('storm_steps', cap = 2)", nil)
	require.NoError(t, err)
	assert.Equal(t, "2026-03-01 00:00:00\t1772323200000\tDateTime('UTC')\t16380\n2026-03-01 01:00:00\t1772326800000\tDateTime('UTC')\t16380\n", string(body))
}

func TestRegister_Refusals(t *testing.T) {
	good := stormField(t)
	for name, f := range map[string]Field{
		"no steps":        {Name: "x"},
		"steps and grids": {Steps: good.Steps[:2], Grids: good.Grids},
		"out of order":    {Steps: []time.Time{good.Steps[1], good.Steps[0]}, Grids: good.Grids[:2]},
		"other grid": {Steps: good.Steps[:2], Grids: []vectorfield.Grid{good.Grids[0], {
			West: 0, North: 90, DLon: 2, DLat: 2, Cols: 180, Rows: 91,
			U: good.Grids[1].U, V: good.Grids[1].V}}},
	} {
		assert.Error(t, Register(introspect.NewRegistry(), "storm", f), name)
	}
	assert.Error(t, Register(introspect.NewRegistry(), "not a name", good))
}

func TestParseInts(t *testing.T) {
	got, err := parseInts("[0, -180,360]")
	require.NoError(t, err)
	assert.Equal(t, []int64{0, -180, 360}, got)
	_, err = parseInts("0,1")
	assert.Error(t, err)
}
