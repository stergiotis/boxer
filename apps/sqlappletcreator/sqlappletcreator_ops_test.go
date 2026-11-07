package sqlappletcreator

import (
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appletstore"
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

func draft(t *testing.T, inst *App) (d Draft) {
	t.Helper()
	d, err := query[appops.None, Draft](t, inst, opGetDraft, appops.None{})
	require.NoError(t, err)
	return
}

func TestOperationsCatalogRegisters(t *testing.T) {
	require.NoError(t, manifest.Operations.Validate())
	m, ok := app.LookupManifest(manifest.Id)
	require.True(t, ok)
	require.NotNil(t, m.Operations, app.DefaultRegistry.OperationsDiagnostic(manifest.Id))
	assert.ElementsMatch(t, []string{opGetDraft, opSetField, opSetBuffer, opSave}, ops.Names())
	for _, name := range ops.Names() {
		spec, found := m.Operations.Lookup(name)
		require.True(t, found, name)
		assert.True(t, spec.Agents, name)
	}
	save, _ := m.Operations.Lookup(opSave)
	assert.True(t, save.Async)
	assert.Equal(t, app.OperationEffectConsequential, save.Effect)
	_, hasExport := m.Operations.Lookup("export")
	assert.False(t, hasExport, "the Powerbox dialog is the person's gesture, not an operation")
}

func TestSetFieldSetsAndNeverToggles(t *testing.T) {
	inst := &App{}
	for _, f := range []struct{ field, value string }{
		{"slug", "by-topic"}, {"Title", "Apps by topic"}, {"summary", "one line"}, {" icon ", "🧩"},
	} {
		for range 2 {
			require.NoError(t, command(t, inst, opSetField, SetFieldArgs{Field: f.field, Value: f.value}))
		}
	}
	d := draft(t, inst)
	assert.Equal(t, "by-topic", d.Slug)
	assert.Equal(t, "Apps by topic", d.Title)
	assert.Equal(t, "one line", d.Summary)
	assert.Equal(t, "🧩", d.Icon)

	require.NoError(t, command(t, inst, opSetField, SetFieldArgs{Field: "icon"}))
	assert.Empty(t, draft(t, inst).Icon, "an empty value clears the field")

	err := command(t, inst, opSetField, SetFieldArgs{Field: "sql", Value: "x"})
	require.NotNil(t, refusal(err))
	assert.Equal(t, reasonField, refusal(err).Reason)
}

func TestSetBufferQuotesTheRevision(t *testing.T) {
	inst := &App{sql: "SELECT 1"}
	d := draft(t, inst)
	assert.Equal(t, "SELECT 1", d.Sql)
	assert.Equal(t, bufferRevision("SELECT 1"), d.Revision)

	require.NoError(t, command(t, inst, opSetBuffer, SetBufferArgs{Sql: "SELECT 2", Revision: d.Revision}))
	assert.Equal(t, "SELECT 2", inst.sql)

	err := command(t, inst, opSetBuffer, SetBufferArgs{Sql: "SELECT 3", Revision: d.Revision})
	require.NotNil(t, refusal(err), "a revision the buffer has moved past is refused")
	assert.Equal(t, reasonRevision, refusal(err).Reason)
	assert.Equal(t, "SELECT 2", inst.sql)
}

func TestSaveSettlesWithTheStoresReply(t *testing.T) {
	for _, tc := range []struct {
		name  string
		bus   *fakeSaveBus
		check func(t *testing.T, o SaveOutcome)
	}{
		{"accepted", &fakeSaveBus{reply: appletstore.SaveReply{OK: true, Class: "read"}}, func(t *testing.T, o SaveOutcome) {
			assert.Equal(t, SaveOutcome{Saved: true, Slug: "my-applet", Class: "read"}, o)
		}},
		{"refused", &fakeSaveBus{reply: appletstore.SaveReply{Error: "slug taken"}}, func(t *testing.T, o SaveOutcome) {
			assert.Equal(t, SaveOutcome{Slug: "my-applet", Refused: "slug taken"}, o)
		}},
		{"transport", &fakeSaveBus{err: errors.New("no responders")}, func(t *testing.T, o SaveOutcome) {
			assert.False(t, o.Saved)
			assert.Contains(t, o.Error, "no responders")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inst := &App{bus: tc.bus, log: zerolog.Nop(), sql: "SELECT 1", slug: "my-applet", title: "My Applet", summary: "one line"}
			require.NoError(t, command(t, inst, opSave, appops.None{}))
			var raw []byte
			require.Eventually(t, func() bool {
				settled, result, err := inst.Operations().(app.OperationsAsyncI).SettleAsync(app.OperationCall{}, opSave, nil)
				require.NoError(t, err)
				raw = result
				return settled
			}, 2*time.Second, 10*time.Millisecond)
			o, err := buscodec.Decode[SaveOutcome](raw)
			require.NoError(t, err)
			tc.check(t, o)
			assert.False(t, draft(t, inst).Saving)
		})
	}
}

func TestSaveRefusesADraftThatDoesNotCompose(t *testing.T) {
	inst := &App{bus: &fakeSaveBus{}, log: zerolog.Nop(), sql: "SELECT 1", slug: "x"}
	err := command(t, inst, opSave, appops.None{})
	require.NotNil(t, refusal(err))
	assert.Contains(t, refusal(err).Reason, "title is required")

	inst = &App{log: zerolog.Nop(), sql: "SELECT 1", slug: "x", title: "X", summary: "y"}
	err = command(t, inst, opSave, appops.None{})
	require.NotNil(t, refusal(err), "no bus is a refusal, not a call left running")
}

func TestSaveIsUnavailableWhileInFlight(t *testing.T) {
	inst := &App{saveBusy: true}
	ok, reason := inst.Operations().Snapshot().Available(opSave)
	assert.False(t, ok)
	assert.Equal(t, reasonSaving, reason)
}
