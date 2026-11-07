package fibscope

import (
	"errors"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/fibscope/launchcfg"
	"github.com/stergiotis/boxer/public/identity/fibonacci"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
)

func opsApp() (inst *App) {
	inst = newApp()
	inst.logger = zerolog.Nop()
	return
}

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

// tag5body7 is the word with tag value 5 and body 7.
var tag5body7 = composeId(identifier.TagValue(5).GetTag(), 7)

func TestOperationsCatalogRegisters(t *testing.T) {
	require.NoError(t, manifest.Operations.Validate())
	m, ok := app.LookupManifest(manifest.Id)
	require.True(t, ok)
	require.NotNil(t, m.Operations, app.DefaultRegistry.OperationsDiagnostic(manifest.Id))
	for _, name := range []string{opGetSplit, opGetSql, opGetTradeoff, opSetId, opSetMaxExp} {
		spec, found := m.Operations.Lookup(name)
		require.True(t, found, name)
		assert.True(t, spec.Agents, name)
	}
	assert.Equal(t, launchcfg.Kind, m.LaunchKind)
}

func TestSetIdThenGetSplit(t *testing.T) {
	inst := opsApp()
	require.NoError(t, command(t, inst, opSetId, SetIdArgs{Id: tag5body7}))
	assert.Equal(t, tag5body7, inst.id)
	sp, err := query[struct{}, Split](t, inst, opGetSplit, struct{}{})
	require.NoError(t, err)
	assert.True(t, sp.Valid)
	assert.Equal(t, uint64(5), sp.TagValue)
	assert.Equal(t, uint64(7), sp.Body)
	assert.Equal(t, sp.TagWidth, len(sp.Code))
	assert.Equal(t, "11", sp.Code[len(sp.Code)-2:], "the code ends in the comma")
	assert.Equal(t, "5 = 5", sp.Zeckendorf)
	assert.LessOrEqual(t, sp.RangeLo, tag5body7)
	assert.GreaterOrEqual(t, sp.RangeHi, tag5body7)
	assert.Empty(t, sp.Note)

	require.NoError(t, command(t, inst, opSetId, SetIdArgs{Id: invalidId}))
	sp, err = query[struct{}, Split](t, inst, opGetSplit, struct{}{})
	require.NoError(t, err)
	assert.False(t, sp.Valid)
	assert.Zero(t, sp.TagValue)
	assert.Contains(t, sp.Note, "no comma")
	assert.Equal(t, "0x000000000000002a", sp.Hex)

	v, ok := inst.Operations().ResourceValue(resId).(uint64)
	require.True(t, ok)
	assert.Equal(t, invalidId, v)
	assert.True(t, inst.Operations().Restore(resId, tag5body7))
	assert.Equal(t, tag5body7, inst.id)
}

func TestGetSqlFoldsTheTagAndRefusesAnInvalidWord(t *testing.T) {
	inst := opsApp()
	out, err := query[struct{}, Sql](t, inst, opGetSql, struct{}{})
	require.NoError(t, err)
	assert.Equal(t, uint64(12), out.TagValue)
	assert.Equal(t, "LW_ID_HAS_TAG(id, 12)", out.Filter)
	assert.NotEmpty(t, out.Expansion)
	assert.NotContains(t, out.Expansion, "LW_ID_HAS_TAG", "the macro is expanded")

	inst.id = invalidId
	_, err = query[struct{}, Sql](t, inst, opGetSql, struct{}{})
	assert.Contains(t, refusal(err).Reason, "not a tagged id")
}

func TestGetTradeoffIsBounded(t *testing.T) {
	inst := opsApp()
	out, err := query[GetTradeoffArgs, Tradeoff](t, inst, opGetTradeoff, GetTradeoffArgs{})
	require.NoError(t, err)
	assert.Equal(t, uint64(1e6), out.MaxExp)
	assert.Len(t, out.Classes, len(fibonacci.WidthClasses()))
	assert.False(t, out.Truncated)
	assert.Positive(t, out.RecommendedWidth)
	assert.LessOrEqual(t, out.TagValueLo, out.TagValueHi)
	recommended := 0
	for _, row := range out.Classes {
		assert.Len(t, row.ExhaustSeconds, len(out.Rates))
		if row.Recommended {
			recommended++
			assert.GreaterOrEqual(t, row.MaxBody, out.MaxExp, "the pick holds max_exp bodies")
		}
	}
	assert.Equal(t, 1, recommended)

	out, err = query[GetTradeoffArgs, Tradeoff](t, inst, opGetTradeoff, GetTradeoffArgs{Limit: 3})
	require.NoError(t, err)
	assert.Len(t, out.Classes, 3)
	assert.True(t, out.Truncated)
	_, err = query[GetTradeoffArgs, Tradeoff](t, inst, opGetTradeoff, GetTradeoffArgs{Limit: -1})
	assert.NotNil(t, refusal(err))
}

func TestSetMaxExpRefusesOutOfRangeNamingTheRange(t *testing.T) {
	inst := opsApp()
	require.NoError(t, command(t, inst, opSetMaxExp, SetMaxExpArgs{MaxExp: 1 << 20}))
	assert.Equal(t, float64(1<<20), inst.maxExp)
	for _, bad := range []uint64{0, 1 << 60} {
		err := command(t, inst, opSetMaxExp, SetMaxExpArgs{MaxExp: bad})
		assert.Equal(t, "max_exp is between 1 and 2^60-1 (1152921504606846975)", refusal(err).Reason)
	}
	assert.Equal(t, float64(1<<20), inst.maxExp, "a refused command changes nothing")
}

func TestMountOpensOnTheLaunchConfigOrTheSeed(t *testing.T) {
	raw, err := buscodec.Encode(launchcfg.FibscopeLaunch{Id: tag5body7})
	require.NoError(t, err)
	inst := opsApp()
	mc := app.NewStaticMountContext(launchcfg.AppId, zerolog.Nop(), nil, nil, nil)
	mc.SetLaunchConfig(raw)
	require.NoError(t, inst.Mount(mc))
	assert.Equal(t, tag5body7, inst.id)

	raw, err = buscodec.Encode(launchcfg.FibscopeLaunch{})
	require.NoError(t, err)
	inst = opsApp()
	mc = app.NewStaticMountContext(launchcfg.AppId, zerolog.Nop(), nil, nil, nil)
	mc.SetLaunchConfig(raw)
	require.NoError(t, inst.Mount(mc))
	assert.Equal(t, uint64(defaultId), inst.id, "a zero id leaves the worked example")

	inst = opsApp()
	mc = app.NewStaticMountContext(launchcfg.AppId, zerolog.Nop(), nil, nil, nil)
	mc.SetLaunchConfig([]byte{0x01, 0x02, 0x03, 0x04})
	err = inst.Mount(mc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode launch config")

	id, err := parseIdSeed("0x2a")
	require.NoError(t, err)
	assert.Equal(t, invalidId, id)
	id, err = parseIdSeed(" 42 ")
	require.NoError(t, err)
	assert.Equal(t, invalidId, id)
	_, err = parseIdSeed("forty-two")
	assert.Error(t, err)
}
