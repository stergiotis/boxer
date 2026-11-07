package terrainscope

import (
	"errors"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/terrainscope/launchcfg"
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

var swissPoints = SetPointsArgs{ObserverLat: 46.9, ObserverLon: 8.2, TargetLat: 46.95, TargetLon: 8.3}

func TestOperationsCatalogRegisters(t *testing.T) {
	require.NoError(t, manifest.Operations.Validate())
	m, ok := app.LookupManifest(manifest.Id)
	require.True(t, ok)
	require.NotNil(t, m.Operations, app.DefaultRegistry.OperationsDiagnostic(manifest.Id))
	for _, name := range []string{opGetView, opGetResult, opSetPoints, opSetParameter, opClearPoints, opRetry, opCancel} {
		spec, found := m.Operations.Lookup(name)
		require.True(t, found, name)
		assert.True(t, spec.Agents, name)
	}
	spec, _ := m.Operations.Lookup(opSetPoints)
	assert.True(t, spec.Async, "set_points settles when the sweep lands")
	spec, _ = m.Operations.Lookup(opCancel)
	assert.Equal(t, app.OperationEffectConsequential, spec.Effect, "cancelling a job is consequential")
	assert.Equal(t, launchcfg.Kind, m.LaunchKind)
}

func TestSetPointsSetsAndNeverToggles(t *testing.T) {
	inst := phaseApp()
	for range 2 {
		require.NoError(t, command(t, inst, opSetPoints, swissPoints))
		assert.Equal(t, selectionStagePt2, inst.stage)
		assert.False(t, inst.haveLaunched, "a repeat arms the sweep again")
		inst.haveLaunched = true
	}
	p, ok := inst.Operations().ResourceValue(resPoints).(Points)
	require.True(t, ok)
	assert.Equal(t, Points{Placed: 2, ObserverLat: 46.9, ObserverLon: 8.2, TargetLat: 46.95, TargetLon: 8.3}, p)

	err := command(t, inst, opSetPoints, SetPointsArgs{ObserverLat: 91, ObserverLon: 8, TargetLat: 46, TargetLon: 8})
	assert.Equal(t, "observer latitude is between -90 and 90 degrees", refusal(err).Reason)
	assert.Equal(t, 46.9, inst.pt1Lat, "a refused command changes nothing")

	require.NoError(t, command(t, inst, opClearPoints, appops.None{}))
	assert.Equal(t, selectionStageNone, inst.stage)
	h := inst.Operations()
	assert.True(t, h.Restore(resPoints, p))
	assert.Equal(t, selectionStagePt2, inst.stage)
	assert.False(t, h.Restore(resPoints, "not points"))
}

func TestSetParameterRefusesOutOfRangeNamingTheRange(t *testing.T) {
	inst := phaseApp()
	require.NoError(t, command(t, inst, opSetParameter, SetParameterArgs{Name: paramSamples, Value: 7.6}))
	assert.Equal(t, 8.0, inst.samples, "samples is rounded")
	require.NoError(t, command(t, inst, opSetParameter, SetParameterArgs{Name: paramSweepStepDeg, Value: 1}))
	assert.Equal(t, 1.0, inst.sweepStepDeg)

	err := command(t, inst, opSetParameter, SetParameterArgs{Name: paramSweepStepDeg, Value: 0.05})
	assert.Equal(t, "sweep_step_deg is between 0.1 and 5", refusal(err).Reason)
	assert.Equal(t, 1.0, inst.sweepStepDeg)
	err = command(t, inst, opSetParameter, SetParameterArgs{Name: paramObserverHeight, Value: 101})
	assert.Equal(t, "observer_height is between 0 and 100", refusal(err).Reason)
	err = command(t, inst, opSetParameter, SetParameterArgs{Name: "eye", Value: 1})
	assert.Contains(t, refusal(err).Reason, "no parameter named eye")
	assert.Contains(t, refusal(err).Reason, paramSigmaTgtHeight)

	params, ok := inst.Operations().ResourceValue(resParameters).(Parameters)
	require.True(t, ok)
	assert.Equal(t, int64(8), params.Samples)
	params.TargetHeight = 3
	assert.True(t, inst.Operations().Restore(resParameters, params))
	assert.Equal(t, 3.0, inst.targetH)
}

func TestGetResultIsBoundedAndStatesTheMethod(t *testing.T) {
	inst := phaseApp()
	_, err := query[GetResultArgs, Result](t, inst, opGetResult, GetResultArgs{})
	assert.Contains(t, refusal(err).Reason, "no result yet")

	inst.stage = selectionStagePt2
	inst.result = synthSweepResult()
	out, err := query[GetResultArgs, Result](t, inst, opGetResult, GetResultArgs{})
	require.NoError(t, err)
	assert.Equal(t, 9, out.TotalRays)
	assert.Len(t, out.Rays, 9)
	assert.False(t, out.Truncated)
	assert.Equal(t, 24, out.Samples)
	assert.Greater(t, out.RangeM, 0.0)
	assert.Equal(t, -2.0, out.Rays[0].BearingOffsetDeg, "rays come by bearing ascending")
	assert.Contains(t, out.Method, "fraction of 24 Monte-Carlo members")
	assert.Contains(t, out.Method, "no earth curvature")

	out, err = query[GetResultArgs, Result](t, inst, opGetResult, GetResultArgs{Limit: 4})
	require.NoError(t, err)
	assert.Len(t, out.Rays, 4)
	assert.True(t, out.Truncated)
	assert.Equal(t, 9, out.TotalRays, "the limit bounds the rows, not the count")

	_, err = query[GetResultArgs, Result](t, inst, opGetResult, GetResultArgs{Limit: -1})
	assert.NotNil(t, refusal(err))

	inst.result = nil
	inst.sweepErr = errors.New("elevation tiles not available")
	_, err = query[GetResultArgs, Result](t, inst, opGetResult, GetResultArgs{})
	assert.Contains(t, refusal(err).Reason, "elevation tiles not available")
}

func TestGetViewReportsThePhaseAndTheInputs(t *testing.T) {
	inst := phaseApp()
	v, err := query[struct{}, View](t, inst, opGetView, struct{}{})
	require.NoError(t, err)
	assert.Equal(t, "idle", v.Phase)
	assert.Equal(t, defaultObserverH, v.Parameters.ObserverHeight)
	assert.False(t, v.HasResult)

	inst.setPoints(swissPoints)
	inst.result = synthSweepResult()
	v, err = query[struct{}, View](t, inst, opGetView, struct{}{})
	require.NoError(t, err)
	assert.Equal(t, "result", v.Phase)
	assert.Equal(t, 2, v.Points.Placed)
	assert.True(t, v.HasResult)
	assert.Equal(t, 9, v.Rays)
}

func TestRetryAndCancelAreAvailableInTheirPhases(t *testing.T) {
	inst := phaseApp()
	snap := inst.Operations().Snapshot()
	ok, reason := snap.Available(opRetry)
	assert.False(t, ok)
	assert.Contains(t, reason, "nothing to retry")
	ok, _ = snap.Available(opCancel)
	assert.False(t, ok)
	assert.Contains(t, refusal(command(t, inst, opCancel, appops.None{})).Reason, "no sweep is running")

	inst.stage = selectionStagePt2
	inst.sweepErr = errors.New("tiles")
	inst.haveLaunched = true
	ok, _ = inst.Operations().Snapshot().Available(opRetry)
	assert.True(t, ok)
	require.NoError(t, command(t, inst, opRetry, appops.None{}))
	assert.False(t, inst.haveLaunched, "retry arms the reactive launch")
}

func TestSetPointsSettlesWhenTheSweepLands(t *testing.T) {
	inst := phaseApp()
	require.NoError(t, command(t, inst, opSetPoints, swissPoints))
	h, ok := inst.Operations().(app.OperationsAsyncI)
	require.True(t, ok)
	settled, _, err := h.SettleAsync(app.OperationCall{}, opSetPoints, nil)
	require.NoError(t, err)
	assert.False(t, settled, "nothing has launched")

	// The frame loop launches and the result lands: generation and landing.
	inst.launchGen++
	inst.result = synthSweepResult()
	inst.landedGen = inst.launchGen
	settled, raw, err := h.SettleAsync(app.OperationCall{}, opSetPoints, nil)
	require.NoError(t, err)
	require.True(t, settled)
	out, err := buscodec.Decode[SweepOutcome](raw)
	require.NoError(t, err)
	assert.Equal(t, SweepOutcome{Phase: "result", Rays: 9, Samples: 24}, out)

	// Cleared meanwhile: the call settles on idle rather than waiting.
	require.NoError(t, command(t, inst, opSetPoints, swissPoints))
	inst.resetSelection()
	settled, raw, err = h.SettleAsync(app.OperationCall{}, opSetPoints, nil)
	require.NoError(t, err)
	require.True(t, settled)
	out, err = buscodec.Decode[SweepOutcome](raw)
	require.NoError(t, err)
	assert.Equal(t, "idle", out.Phase)
}

func TestMountAppliesTheLaunchConfig(t *testing.T) {
	raw, err := buscodec.Encode(launchcfg.TerrainscopeLaunch{ObserverLat: 46.9, ObserverLon: 8.2, TargetLat: 46.95, TargetLon: 8.3,
		ObserverHeight: 2.5, Samples: 4})
	require.NoError(t, err)
	inst := phaseApp()
	mc := app.NewStaticMountContext(launchcfg.AppId, zerolog.Nop(), nil, nil, nil)
	mc.SetLaunchConfig(raw)
	require.NoError(t, inst.Mount(mc))
	assert.Equal(t, selectionStagePt2, inst.stage)
	assert.Equal(t, 46.95, inst.pt2Lat)
	assert.Equal(t, 2.5, inst.observerH)
	assert.Equal(t, 4.0, inst.samples)
	assert.Equal(t, defaultTargetH, inst.targetH, "a zero height leaves the default")
	assert.NotNil(t, inst.phaseChip)
}

func TestMountRefusesABadLaunchConfig(t *testing.T) {
	inst := phaseApp()
	mc := app.NewStaticMountContext(launchcfg.AppId, zerolog.Nop(), nil, nil, nil)
	mc.SetLaunchConfig([]byte{0x01, 0x02, 0x03, 0x04})
	err := inst.Mount(mc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode launch config")

	raw, err := buscodec.Encode(launchcfg.TerrainscopeLaunch{ObserverLat: 46.9, ObserverLon: 8.2, TargetLat: 46.95, TargetLon: 8.3, Samples: 500})
	require.NoError(t, err)
	inst = phaseApp()
	mc = app.NewStaticMountContext(launchcfg.AppId, zerolog.Nop(), nil, nil, nil)
	mc.SetLaunchConfig(raw)
	err = inst.Mount(mc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "samples is between 0 and 64")
	assert.Equal(t, selectionStageNone, inst.stage, "a refused config opens nothing")
}

func TestMountOpensPlainlyWithoutAConfig(t *testing.T) {
	inst := phaseApp()
	mc := app.NewStaticMountContext(launchcfg.AppId, zerolog.Nop(), nil, nil, nil)
	require.NoError(t, inst.Mount(mc))
	assert.Equal(t, selectionStageNone, inst.stage)
	assert.Empty(t, inst.seedErr)
}

func TestParsePointsSeed(t *testing.T) {
	in, err := parsePointsSeed("46.9, 8.2, 46.95, 8.3")
	require.NoError(t, err)
	assert.Equal(t, swissPoints, in)
	in, err = parsePointsSeed("46.9 8.2 46.95 8.3")
	require.NoError(t, err)
	assert.Equal(t, swissPoints, in)
	_, err = parsePointsSeed("46.9,8.2,46.95")
	assert.ErrorContains(t, err, "3 numbers")
	_, err = parsePointsSeed("46.9,8.2,46.95,east")
	assert.ErrorContains(t, err, "not a number")
	_, err = parsePointsSeed("46.9,8.2,95,8.3")
	assert.ErrorContains(t, err, "target latitude")
}
