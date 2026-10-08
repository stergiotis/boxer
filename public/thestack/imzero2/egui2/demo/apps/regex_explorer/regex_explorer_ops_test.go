package regex_explorer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
)

func TestTheCatalogRegisters(t *testing.T) {
	require.NoError(t, manifest.Operations.Validate())
	m, ok := app.LookupManifest(manifest.Id)
	require.True(t, ok)
	require.NotNil(t, m.Operations, app.DefaultRegistry.OperationsDiagnostic(manifest.Id))
	for _, name := range []string{opGetState, opSetInputs, opShowTab, opApplyShowcase, opGetMatches, opGetFunctions, opGetMulti, opGetEngineCheck} {
		spec, found := m.Operations.Lookup(name)
		require.True(t, found, name)
		assert.True(t, spec.Agents, name)
	}
}

// command runs a command through the untyped handler, as the dispatcher
// does: arguments and result travel CBOR-encoded.
func command[In any](t *testing.T, h app.OperationsHandlerI, name string, in In) (err error) {
	t.Helper()
	args, encErr := buscodec.Encode(in)
	require.NoError(t, encErr)
	_, err = h.ApplyCommand(app.OperationCall{}, name, args)
	return
}

// query takes a snapshot, as the host does after the command stage, and
// runs a query against it.
func query[Out any](t *testing.T, h app.OperationsHandlerI, name string) (out Out) {
	t.Helper()
	args, err := buscodec.Encode(struct{}{})
	require.NoError(t, err)
	raw, err := h.Snapshot().Query(name, args)
	require.NoError(t, err)
	out, err = buscodec.Decode[Out](raw)
	require.NoError(t, err)
	return
}

func TestOperationsSetAndReadTheInputs(t *testing.T) {
	inst := newTestApp(t)
	h := ops.Bind(inst)

	pattern, haystack, ci := `(\w+)@([\w.]+)`, "alice@example.com bob@test.org", true
	require.NoError(t, command(t, h, opSetInputs, SetInputsArgs{Pattern: &pattern, Haystack: &haystack, CaseInsensitive: &ci}))
	assert.Equal(t, pattern, inst.pattern)
	assert.True(t, inst.caseInsensitive)
	assert.True(t, inst.dotAll, "a flag left out keeps its value")

	st := query[State](t, h, opGetState)
	assert.Equal(t, 2, st.Matches)
	assert.Equal(t, "(?is)"+pattern, st.EffectivePattern)
	assert.Equal(t, "matches", st.Tab)
	assert.NotEmpty(t, st.Showcases)

	m := query[MatchesResult](t, h, opGetMatches)
	require.True(t, m.Valid)
	require.Len(t, m.Matches, 2)
	assert.Equal(t, "bob@test.org", m.Matches[1].Text)
	assert.Equal(t, "test.org", m.Matches[1].Groups[1].Text)
}

func TestOperationsRefuseWhatTheyCannotDo(t *testing.T) {
	inst := newTestApp(t)
	h := ops.Bind(inst)

	require.NoError(t, command(t, h, opShowTab, ShowTabArgs{Tab: "Functions"}))
	assert.Equal(t, tabFunctions, inst.tab)
	assert.Error(t, command(t, h, opShowTab, ShowTabArgs{Tab: "nope"}))

	require.NoError(t, command(t, h, opApplyShowcase, ApplyShowcaseArgs{Name: showcaseCases[0].Title}))
	assert.Equal(t, showcaseCases[0].Pattern, inst.pattern)
	assert.Error(t, command(t, h, opApplyShowcase, ApplyShowcaseArgs{Name: "no such showcase"}))
}

// TestGetFunctionsReportsPendingThenTheAnswer pins that get_functions
// reads the window's lane and never asks ClickHouse itself: pending until
// the lane holds an answer for the current inputs, then the comparison.
func TestGetFunctionsReportsPendingThenTheAnswer(t *testing.T) {
	inst := newTestApp(t)
	h := ops.Bind(inst)
	pattern, haystack := `\d+`, "a1 b22"
	require.NoError(t, command(t, h, opSetInputs, SetInputsArgs{Pattern: &pattern, Haystack: &haystack}))

	f := query[FunctionsResult](t, h, opGetFunctions)
	require.True(t, f.Valid)
	assert.True(t, f.Pending)
	require.NotEmpty(t, f.Rows)
	assert.Equal(t, "match", f.Rows[0].Function)
	assert.Equal(t, "1", f.Rows[0].Prediction)

	// ClickHouse answers — as the lane would hold it — exactly as predicted.
	inst.fnLane.serve(inst.singleKey(), predictFunctions(inst.analysis()))
	inst.replaceLane.serve(inst.replaceKey(), replaceOutcome{One: "a b22", All: "a b"})
	f = query[FunctionsResult](t, h, opGetFunctions)
	assert.False(t, f.Pending)
	for _, r := range f.Rows {
		if r.Modelled {
			assert.True(t, r.Agrees, r.Function)
		}
	}
}

func TestGetMultiMarksLines(t *testing.T) {
	inst := newTestApp(t)
	h := ops.Bind(inst)
	list, haystack := "a+\n(unclosed\nz", "aaa"
	require.NoError(t, command(t, h, opSetInputs, SetInputsArgs{PatternList: &list, Haystack: &haystack}))

	mr := query[MultiResult](t, h, opGetMulti)
	require.Len(t, mr.Lines, 3)
	assert.Equal(t, "invalid", mr.Lines[1].Status)
	assert.NotEmpty(t, mr.Lines[1].Message)
	assert.Equal(t, "pending", mr.Lines[0].Status)
}
