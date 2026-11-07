package writingstylescope

import (
	"errors"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/writingstylescope/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
)

func query[In any, Out any](t *testing.T, inst *App, name string, in In) (out Out, err error) {
	t.Helper()
	args, encErr := buscodec.Encode(in)
	require.NoError(t, encErr)
	raw, err := inst.Operations().Snapshot().Query(name, args)
	if err != nil {
		return
	}
	out, decErr := buscodec.Decode[Out](raw)
	require.NoError(t, decErr)
	return
}

func command[In any](t *testing.T, inst *App, name string, in In) (err error) {
	t.Helper()
	args, encErr := buscodec.Encode(in)
	require.NoError(t, encErr)
	_, err = inst.Operations().ApplyCommand(app.OperationCall{}, name, args)
	return
}

func refusal(err error) (r *app.OperationRefusal) {
	errors.As(err, &r)
	return
}

func TestOperationsCatalogRegisters(t *testing.T) {
	require.NoError(t, manifest.Operations.Validate())
	m, ok := app.LookupManifest(manifest.Id)
	require.True(t, ok)
	require.NotNil(t, m.Operations, app.DefaultRegistry.OperationsDiagnostic(manifest.Id))
	for _, name := range []string{opGetView, opGetPairs, opGetDistribution, opSetDocument, opSetFloor, opAnalyze, opOpenInPlay} {
		spec, found := m.Operations.Lookup(name)
		require.True(t, found, name)
		assert.True(t, spec.Agents, name)
	}
	spec, _ := m.Operations.Lookup(opAnalyze)
	assert.True(t, spec.Async, "analyze settles when the sweep lands")
	spec, _ = m.Operations.Lookup(opOpenInPlay)
	assert.Equal(t, app.OperationEffectConsequential, spec.Effect, "the handover writes outside the window")
	spec, _ = m.Operations.Lookup(opGetPairs)
	assert.True(t, spec.Untrusted, "section headings are the documents' text")
	assert.Equal(t, launchcfg.Kind, m.LaunchKind)
}

// set_document quotes the revision it read; a text that moved on since is
// refused and nothing changes.
func TestSetDocumentHoldsTheRevision(t *testing.T) {
	inst := phaseApp()
	v, err := query[struct{}, View](t, inst, opGetView, struct{}{})
	require.NoError(t, err)
	require.NoError(t, command(t, inst, opSetDocument, SetDocumentArgs{Which: docB, Revision: v.RevisionB, Text: "# B\n\nnew"}))
	assert.Equal(t, "# B\n\nnew", inst.docB)

	err = command(t, inst, opSetDocument, SetDocumentArgs{Which: docB, Revision: v.RevisionB, Text: "again"})
	assert.Contains(t, refusal(err).Reason, "changed since that revision")
	assert.Equal(t, "# B\n\nnew", inst.docB, "a refused write changes nothing")

	err = command(t, inst, opSetDocument, SetDocumentArgs{Which: "C", Revision: v.RevisionA, Text: "x"})
	assert.Equal(t, "which is A or B", refusal(err).Reason)
	err = command(t, inst, opSetDocument, SetDocumentArgs{Which: docA, Revision: v.RevisionA, Text: strings.Repeat("x", maxPaneBytes+1)})
	assert.Contains(t, refusal(err).Reason, "over the pane's cap")

	d, ok := inst.Operations().ResourceValue(resDocuments).(Documents)
	require.True(t, ok)
	assert.Equal(t, Documents{A: sampleDocA, B: "# B\n\nnew"}, d)
	assert.True(t, inst.Operations().Restore(resDocuments, Documents{A: "a", B: "b"}))
	assert.Equal(t, "b", inst.docB)
}

func TestSetFloorRefusesOutOfRangeNamingTheRange(t *testing.T) {
	inst := phaseApp()
	require.NoError(t, command(t, inst, opSetFloor, SetFloorArgs{Floor: 120.4}))
	assert.Equal(t, 120.0, inst.minSectionBytes)
	err := command(t, inst, opSetFloor, SetFloorArgs{Floor: 4001})
	assert.Equal(t, "floor is between 0 and 4000 bytes", refusal(err).Reason)
	err = command(t, inst, opSetFloor, SetFloorArgs{Floor: -1})
	assert.Equal(t, "floor is between 0 and 4000 bytes", refusal(err).Reason)
	assert.Equal(t, 120.0, inst.minSectionBytes)
}

func TestMatrixQueriesAreBoundedAndStateTheMethod(t *testing.T) {
	inst := phaseApp()
	_, err := query[GetPairsArgs, PairsResult](t, inst, opGetPairs, GetPairsArgs{})
	assert.Contains(t, refusal(err).Reason, "no result yet")

	inst.analyze()
	out, err := query[GetPairsArgs, PairsResult](t, inst, opGetPairs, GetPairsArgs{Limit: 3})
	require.NoError(t, err)
	require.Len(t, out.Pairs, 3)
	assert.Equal(t, inst.res.Rows()*inst.res.Cols(), out.Total)
	assert.True(t, out.Truncated)
	assert.Equal(t, 1, out.Pairs[0].Rank)
	assert.LessOrEqual(t, out.Pairs[0].Ncd, out.Pairs[1].Ncd, "ranked closest first")
	assert.GreaterOrEqual(t, out.Pairs[0].Beats, out.Pairs[1].Beats, "the closest pair beats the most")
	assert.Greater(t, out.Pairs[0].Beats, 0.5)
	assert.Contains(t, out.Method, "no absolute scale")
	assert.False(t, out.Stale)

	out, err = query[GetPairsArgs, PairsResult](t, inst, opGetPairs, GetPairsArgs{Limit: 100000})
	require.NoError(t, err)
	assert.LessOrEqual(t, len(out.Pairs), opsMaxPairs)
	assert.Equal(t, out.Total > len(out.Pairs), out.Truncated)
	_, err = query[GetPairsArgs, PairsResult](t, inst, opGetPairs, GetPairsArgs{Limit: -1})
	assert.NotNil(t, refusal(err))

	dist, err := query[GetDistributionArgs, Distribution](t, inst, opGetDistribution, GetDistributionArgs{Points: 5})
	require.NoError(t, err)
	require.Len(t, dist.Quantiles, 5)
	assert.Equal(t, dist.Min, dist.Quantiles[0].Ncd)
	assert.Equal(t, dist.Max, dist.Quantiles[4].Ncd)
	assert.False(t, dist.Truncated)
	assert.Contains(t, dist.Method, "not independent")
	dist, err = query[GetDistributionArgs, Distribution](t, inst, opGetDistribution, GetDistributionArgs{Points: 500})
	require.NoError(t, err)
	assert.Len(t, dist.Quantiles, opsMaxQuantiles)
	assert.True(t, dist.Truncated)
	_, err = query[GetDistributionArgs, Distribution](t, inst, opGetDistribution, GetDistributionArgs{Points: 1})
	assert.Equal(t, "points is at least 2", refusal(err).Reason)

	inst.docA += "\n\nedited"
	out, err = query[GetPairsArgs, PairsResult](t, inst, opGetPairs, GetPairsArgs{})
	require.NoError(t, err)
	assert.True(t, out.Stale, "the matrix is read with its staleness")
}

func TestAnalyzeSettlesWhenTheSweepLands(t *testing.T) {
	inst := phaseApp()
	h, ok := inst.Operations().(app.OperationsAsyncI)
	require.True(t, ok)
	inst.analyzeWant = 1
	settled, _, err := h.SettleAsync(app.OperationCall{}, opAnalyze, nil)
	require.NoError(t, err)
	assert.False(t, settled, "nothing has landed")

	require.NoError(t, command(t, inst, opAnalyze, appops.None{}))
	settled, raw, err := h.SettleAsync(app.OperationCall{}, opAnalyze, nil)
	require.NoError(t, err)
	require.True(t, settled)
	out, err := buscodec.Decode[AnalyzeOutcome](raw)
	require.NoError(t, err)
	assert.Equal(t, "result", out.Phase)
	assert.Equal(t, inst.res.Rows()*inst.res.Cols(), out.Pairs)

	v, err := query[struct{}, View](t, inst, opGetView, struct{}{})
	require.NoError(t, err)
	assert.Equal(t, "result", v.Phase)
	assert.True(t, v.HasResult)
	assert.Equal(t, out.Pairs, v.Pairs)
}

func TestOpenInPlayNeedsAResultAndABus(t *testing.T) {
	inst := phaseApp()
	ok, reason := inst.Operations().Snapshot().Available(opOpenInPlay)
	assert.False(t, ok)
	assert.Contains(t, reason, "analyze first")
	inst.analyze()
	err := command(t, inst, opOpenInPlay, appops.None{})
	assert.Contains(t, refusal(err).Reason, "no bus")
}

func TestMountAppliesTheLaunchConfig(t *testing.T) {
	raw, err := buscodec.Encode(launchcfg.WritingstylescopeLaunch{DocA: "# A\n\none", DocB: "# B\n\ntwo", Floor: 80})
	require.NoError(t, err)
	inst := phaseApp()
	mc := app.NewStaticMountContext(launchcfg.AppId, zerolog.Nop(), nil, nil, nil)
	mc.SetLaunchConfig(raw)
	require.NoError(t, inst.Mount(mc))
	assert.Equal(t, "# A\n\none", inst.docA)
	assert.Equal(t, "# B\n\ntwo", inst.docB)
	assert.Equal(t, 80.0, inst.minSectionBytes)
	assert.Nil(t, inst.storage, "a handed-over window neither restores nor persists")
	assert.NotNil(t, inst.phaseChip)

	inst = phaseApp()
	mc = app.NewStaticMountContext(launchcfg.AppId, zerolog.Nop(), nil, nil, nil)
	mc.SetLaunchConfig([]byte{0x01, 0x02, 0x03, 0x04})
	err = inst.Mount(mc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode launch config")

	raw, err = buscodec.Encode(launchcfg.WritingstylescopeLaunch{DocA: "a", DocB: "b", Floor: 9000})
	require.NoError(t, err)
	inst = phaseApp()
	mc = app.NewStaticMountContext(launchcfg.AppId, zerolog.Nop(), nil, nil, nil)
	mc.SetLaunchConfig(raw)
	err = inst.Mount(mc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "floor is between 0 and 4000 bytes")
	assert.Equal(t, sampleDocA, inst.docA, "a refused config opens nothing")
}

func TestReadDocsSeed(t *testing.T) {
	ta, tb, err := readDocsSeed(`{"DocA":"# A","DocB":"# B"}`)
	require.NoError(t, err)
	assert.Equal(t, "# A", ta)
	assert.Equal(t, "# B", tb)
	_, _, err = readDocsSeed(`{"DocA":"# A"}`)
	assert.ErrorContains(t, err, "needs both DocA and DocB")
	_, _, err = readDocsSeed("a.md:b.md")
	assert.ErrorContains(t, err, "not a JSON object")
}
