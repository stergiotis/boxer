package trail_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore/chstoretest"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/semistructured/leeway/factsschema"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
	"github.com/stergiotis/boxer/public/storage/recordstore/recordstoretest"
)

// The ditch row is within the audit event's bounds, ok or failed, with the
// app filter when given and the error, bounded, when it failed.
func TestDitchEvent(t *testing.T) {
	ok := trail.DitchEvent("", nil)
	assert.Equal(t, trail.TrailDomain, ok.Domain)
	assert.Equal(t, trail.ActionAuditDitch, ok.Action)
	assert.Equal(t, trail.OutcomeOk, ok.Outcome)
	assert.Empty(t, ok.AttrKeys)
	assert.Empty(t, ok.RefTypes, "the ditch names no entity")

	failed := trail.DitchEvent("apps/x", errors.New(strings.Repeat("e", 2*trail.MaxValueRunes)))
	assert.Equal(t, trail.OutcomeFailed, failed.Outcome)
	assert.Equal(t, []string{"app", "error"}, failed.AttrKeys)
	assert.Equal(t, "apps/x", failed.AttrValues[0])
	assert.Len(t, []rune(failed.AttrValues[1]), trail.MaxValueRunes)
	for _, row := range []trail.AuditEvent{ok, failed} {
		row.PrincipalBy, row.OriginBy = trail.PrincipalByNone, evidence.OriginByHost.String()
		assert.NoError(t, row.Validate())
	}
}

// A recorder with no store does not ditch: the path never mutates without
// its row.
func TestDitchNeedsADurableTrail(t *testing.T) {
	var none *trail.Recorder
	require.Error(t, none.DitchBodies(context.Background(), ""))
	rec := trail.NewRecorder(nil, "run-1", zerolog.Nop())
	defer rec.Close()
	require.Error(t, rec.DitchBodies(context.Background(), ""))
}

// Under an insert transform for the facts table the mutation is not sent:
// it would name a relation that holds none of the rows. The ditch is
// recorded as failed (ADR-0402 §SD5).
func TestDitchIsRefusedUnderATransform(t *testing.T) {
	var execd []string
	inserts := 0
	exec := &recordstoretest.HookExecutor{
		OnExec: func(_ context.Context, sql string) error { execd = append(execd, sql); return nil },
		OnInsertArrowWith: func(context.Context, string, []arrow.RecordBatch, recordstore.InsertSettings) (bool, error) {
			inserts++
			return true, nil
		},
	}
	rec := trail.NewRecorder(exec, "run-t", zerolog.Nop())
	defer rec.Close()
	unregister := recordstore.RegisterInsertTransform(trail.TrailTableName, recordstore.InsertTransformFunc(
		func(_ string, records []arrow.RecordBatch) (string, []arrow.RecordBatch, error) {
			for _, r := range records {
				r.Retain()
			}
			return "carrier.rows", records, nil
		}))
	defer unregister()
	err := rec.DitchBodies(context.Background(), "")
	require.ErrorIs(t, err, recordstore.ErrTransformedTable)
	for _, s := range execd {
		assert.NotContains(t, s, "ALTER TABLE")
	}
	assert.Positive(t, inserts, "the failed ditch is recorded")
}

// refuseAlter is a server that refuses mutations.
type refuseAlter struct {
	recordstore.ExecutorI
}

func (inst refuseAlter) Exec(ctx context.Context, sql string) error {
	if strings.HasPrefix(sql, "ALTER TABLE") {
		return errors.New("mutations are not allowed")
	}
	return inst.ExecutorI.Exec(ctx, sql)
}

// Over clickhouse-local: a ditch through the recorder lands its row, ok
// when the statement ran and failed with the error when it did not.
func TestDitchBodiesRecordsTheDitch(t *testing.T) {
	local, ctx := localFacts(t)
	rec := trail.NewRecorder(local, "run-d", zerolog.Nop())
	defer rec.Close()
	require.NoError(t, rec.DitchBodies(ctx, "apps/x"))
	refusing := trail.NewRecorder(refuseAlter{ExecutorI: local}, "run-e", zerolog.Nop())
	defer refusing.Close()
	err := refusing.DitchBodies(ctx, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutations are not allowed")

	byOutcome := map[string]trail.AuditEvent{}
	for _, e := range scanEvents(t, rec, ctx, recordstore.ScanOpts{}) {
		if e.AuditEvent.Val.Action == trail.ActionAuditDitch {
			byOutcome[e.AuditEvent.Val.Outcome] = e.AuditEvent.Val
			assert.Equal(t, "", e.Origin.Val.App, "the recorder's own row")
		}
	}
	require.Len(t, byOutcome, 2)
	assert.Equal(t, []string{"app"}, byOutcome[trail.OutcomeOk].AttrKeys)
	assert.Equal(t, []string{"apps/x"}, byOutcome[trail.OutcomeOk].AttrValues)
	assert.Equal(t, []string{"error"}, byOutcome[trail.OutcomeFailed].AttrKeys)
	assert.Contains(t, byOutcome[trail.OutcomeFailed].AttrValues[0], "mutations are not allowed")
	assert.Equal(t, trail.TrailDomain, byOutcome[trail.OutcomeFailed].Domain)
}

// Over clickhouse-local, with the facts stored in a carrier whose transform
// answers the removal: the statement that runs is the carrier's, it empties
// the text of the tenant's message rows and of no other row, and the ditch
// is recorded ok through the same transform (ADR-0402 §SD5).
func TestDitchBodiesRunsTheDeploymentsStatement(t *testing.T) {
	local, ctx := localFacts(t)
	contract, err := chstore.SchemaContract(chstore.Config{Database: factsschema.DatabaseName, Table: factsschema.TableName})
	require.NoError(t, err)
	carrier := chstoretest.NewCarrier(contract.Columns, "carrier.rows", "carrier.facts_view", "one")
	carrier.InPlace = true
	for _, stmt := range append(carrier.SetupStatements(), carrier.ViewStatement(nil, nil)) {
		require.NoError(t, local.Exec(ctx, stmt))
	}
	col := carrier.FactsColumn
	const (
		id      = "id:id:u64:47::0:"
		symLr   = "tv:symbol:lr:lr:u64:1247:::0::data"
		symVal  = "tv:symbol:value:val:s:124::I:0::data"
		textVal = "tv:textArray:value:val:sh:5::7:0::data"
		textLen = "tv:textArray:len:len:u64:4D:::0::data"
	)
	for _, c := range []string{id, symLr, symVal, textVal, textLen} {
		require.NotEmpty(t, col(c), c)
	}
	message := strconv.FormatUint(trail.TrailMembershipIds["LlmMessage"]["runtimeKindLlmMessage"], 10)
	insert := func(tenant string, rowId int, kind string) {
		require.NoError(t, local.Exec(ctx, "INSERT INTO carrier.rows (tenant, "+col(id)+", "+col(symLr)+", "+col(symVal)+", "+col(textVal)+", "+col(textLen)+
			") VALUES ('"+tenant+"', "+strconv.Itoa(rowId)+", ["+kind+"], ['k'], ['kept text'], [1])"))
	}
	insert("one", 1, message)     // the tenant's message: ditched
	insert("one", 2, "424242")    // the tenant's row of another kind: kept
	insert("another", 3, message) // another tenant's message: not a fact, kept

	unregister := recordstore.RegisterInsertTransform(trail.TrailTableName, carrier.Transform())
	defer unregister()
	var ran []string
	rec := trail.NewRecorder(observeExec{ExecutorI: local, ran: &ran}, "run-ip", zerolog.Nop())
	defer rec.Close()
	require.NoError(t, rec.DitchBodies(ctx, ""))

	require.Len(t, ran, 1)
	assert.True(t, strings.HasPrefix(ran[0], "ALTER TABLE carrier.rows UPDATE "), ran[0][:min(len(ran[0]), 80)])
	assert.Contains(t, ran[0], "WHERE tenant = 'one' AND (")
	assert.NotContains(t, ran[0], "boxer.facts")
	textOf := func(rowId int) (n string) {
		for r, qerr := range local.QueryArrow(ctx, "SELECT toString(length("+col(textVal)+") + length("+col(textLen)+")) AS n FROM carrier.rows WHERE "+col(id)+" = "+strconv.Itoa(rowId)+" SETTINGS output_format_arrow_string_as_string=1") {
			require.NoError(t, qerr)
			n = r.Column(0).(*array.String).Value(0)
			r.Release()
		}
		return
	}
	assert.Equal(t, "0", textOf(1), "the message's text section is empty")
	assert.Equal(t, "2", textOf(2), "a row of another kind keeps its text")
	assert.Equal(t, "2", textOf(3), "another tenant's row is not touched")

	// The ditch event went through the transform into the carrier.
	for r, qerr := range local.QueryArrow(ctx, "SELECT count() AS n FROM carrier.rows WHERE tenant = 'one'") {
		require.NoError(t, qerr)
		assert.Greater(t, r.Column(0).(*array.Uint64).Value(0), uint64(2), "the two rows, and the recorded ditch with its seal")
		r.Release()
	}
}

// observeExec records the ALTER statements sent.
type observeExec struct {
	recordstore.ExecutorI
	ran *[]string
}

func (inst observeExec) Exec(ctx context.Context, sql string) error {
	if strings.HasPrefix(sql, "ALTER TABLE") {
		*inst.ran = append(*inst.ran, sql)
	}
	return inst.ExecutorI.Exec(ctx, sql)
}
