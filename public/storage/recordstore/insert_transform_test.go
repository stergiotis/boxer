package recordstore_test

import (
	"context"
	"errors"
	"iter"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/recordstoretest"
)

// oneColumn is a batch of one int64 column named name holding vals.
func oneColumn(alloc memory.Allocator, name string, vals ...int64) arrow.RecordBatch {
	schema := arrow.NewSchema([]arrow.Field{{Name: name, Type: arrow.PrimitiveTypes.Int64}}, nil)
	b := array.NewInt64Builder(alloc)
	defer b.Release()
	b.AppendValues(vals, nil)
	arr := b.NewArray()
	defer arr.Release()
	return array.NewRecordBatch(schema, []arrow.Array{arr}, int64(len(vals)))
}

// renameTo is a transform that sends the batches to target with their one
// column renamed to "carried", built in alloc.
func renameTo(alloc memory.Allocator, target string) recordstore.InsertTransformFunc {
	return func(_ string, records []arrow.RecordBatch) (string, []arrow.RecordBatch, error) {
		out := make([]arrow.RecordBatch, 0, len(records))
		for _, r := range records {
			col := r.Column(0).(*array.Int64)
			out = append(out, oneColumn(alloc, "carried", col.Int64Values()...))
		}
		return target, out, nil
	}
}

type sentInsert struct {
	table    string
	columns  []string
	rows     int64
	settings recordstore.InsertSettings
}

// recorder is an inner executor that records each insert.
func recorder(sent *[]sentInsert) *recordstoretest.HookExecutor {
	return &recordstoretest.HookExecutor{
		OnInsertArrowWith: func(_ context.Context, table string, records []arrow.RecordBatch, settings recordstore.InsertSettings) (bool, error) {
			s := sentInsert{table: table, settings: settings}
			for _, r := range records {
				s.rows += r.NumRows()
				s.columns = s.columns[:0]
				for _, f := range r.Schema().Fields() {
					s.columns = append(s.columns, f.Name)
				}
			}
			*sent = append(*sent, s)
			return true, nil
		},
	}
}

func TestTransformExecutor_RewritesOnlyTheTransformedTable(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer alloc.AssertSize(t, 0)
	var sent []sentInsert
	exec := recordstore.TransformExecutor(recorder(&sent), "boxer.facts", renameTo(alloc, "carrier.rows"))

	in := oneColumn(alloc, "id", 1, 2, 3)
	defer in.Release()
	settings := recordstore.InsertSettings{AsyncInsert: recordstore.ToggleOff}.WithDeduplicationToken("tok-1")
	applied, err := exec.InsertArrowWith(context.Background(), "boxer.facts", []arrow.RecordBatch{in}, settings)
	require.NoError(t, err)
	assert.True(t, applied)
	require.NoError(t, exec.InsertArrow(context.Background(), "boxer.other", []arrow.RecordBatch{in}))

	require.Len(t, sent, 2)
	assert.Equal(t, sentInsert{table: "carrier.rows", columns: []string{"carried"}, rows: 3, settings: settings}, sent[0],
		"the facts insert reaches the inner executor under the transform's table and schema, with its settings intact")
	assert.Equal(t, sentInsert{table: "boxer.other", columns: []string{"id"}, rows: 3}, sent[1],
		"an insert into another table is not transformed")
}

func TestTransformExecutor_ForwardsStatementsAndQueries(t *testing.T) {
	var execd, queried string
	inner := &recordstoretest.HookExecutor{
		OnExec: func(_ context.Context, sql string) error { execd = sql; return nil },
	}
	inner.OnQueryArrow = func(_ context.Context, sql string) iter.Seq2[arrow.RecordBatch, error] {
		queried = sql
		return func(func(arrow.RecordBatch, error) bool) {}
	}
	exec := recordstore.TransformExecutor(inner, "boxer.facts", renameTo(memory.NewGoAllocator(), "carrier.rows"))
	require.NoError(t, exec.Exec(context.Background(), "ALTER TABLE boxer.facts DELETE WHERE 1"))
	for rec, err := range exec.QueryArrow(context.Background(), "SELECT * FROM boxer.facts") {
		require.NoError(t, err)
		rec.Release()
	}
	assert.Equal(t, "ALTER TABLE boxer.facts DELETE WHERE 1", execd)
	assert.Equal(t, "SELECT * FROM boxer.facts", queried)
}

func TestTransformExecutor_RefusesBeforeSending(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer alloc.AssertSize(t, 0)
	in := oneColumn(alloc, "id", 1)
	defer in.Release()
	for name, tr := range map[string]recordstore.InsertTransformI{
		"transform error": recordstore.InsertTransformFunc(func(string, []arrow.RecordBatch) (string, []arrow.RecordBatch, error) {
			return "", nil, errors.New("no mapping for this kind")
		}),
		"malformed target": renameTo(alloc, "carrier.rows; DROP TABLE x"),
		"empty target":     renameTo(alloc, ""),
	} {
		t.Run(name, func(t *testing.T) {
			var sent []sentInsert
			exec := recordstore.TransformExecutor(recorder(&sent), "boxer.facts", tr)
			err := exec.InsertArrow(context.Background(), "boxer.facts", []arrow.RecordBatch{in})
			require.Error(t, err)
			assert.Empty(t, sent, "nothing reaches the server when the transform fails")
		})
	}
}

func TestTransformExecutor_EmptyInsertIsNotTransformed(t *testing.T) {
	called := false
	var sent []sentInsert
	exec := recordstore.TransformExecutor(recorder(&sent), "boxer.facts", recordstore.InsertTransformFunc(func(string, []arrow.RecordBatch) (string, []arrow.RecordBatch, error) {
		called = true
		return "carrier.rows", nil, nil
	}))
	require.NoError(t, exec.InsertArrow(context.Background(), "boxer.facts", nil))
	assert.False(t, called)
	require.Len(t, sent, 1)
	assert.Equal(t, "boxer.facts", sent[0].table)
}

func TestRegisterInsertTransform(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer alloc.AssertSize(t, 0)
	var sent []sentInsert
	exec := recordstore.RegisteredTransforms(recorder(&sent))
	in := oneColumn(alloc, "id", 7)
	defer in.Release()

	require.NoError(t, exec.InsertArrow(context.Background(), "scratch.facts", []arrow.RecordBatch{in}))
	unregister := recordstore.RegisterInsertTransform("scratch.facts", renameTo(alloc, "scratch.carrier"))
	_, ok := recordstore.RegisteredInsertTransform("scratch.facts")
	assert.True(t, ok)
	assert.Panics(t, func() { recordstore.RegisterInsertTransform("scratch.facts", renameTo(alloc, "x.y")) }, "a second transform for one table is refused")
	assert.Panics(t, func() { recordstore.RegisterInsertTransform("not a ref", renameTo(alloc, "x.y")) })
	assert.Panics(t, func() { recordstore.RegisterInsertTransform("scratch.other", nil) })
	require.NoError(t, exec.InsertArrow(context.Background(), "scratch.facts", []arrow.RecordBatch{in}))
	unregister()
	_, ok = recordstore.RegisteredInsertTransform("scratch.facts")
	assert.False(t, ok)
	require.NoError(t, exec.InsertArrow(context.Background(), "scratch.facts", []arrow.RecordBatch{in}))

	require.Len(t, sent, 3)
	assert.Equal(t, []string{"scratch.facts", "scratch.carrier", "scratch.facts"}, []string{sent[0].table, sent[1].table, sent[2].table},
		"the registry is read at each insert")
}

func TestCanonicalTableRef(t *testing.T) {
	for _, c := range []struct {
		ref       string
		canon     string
		qualified bool
		ok        bool
	}{
		{"boxer.facts", "boxer.facts", true, true},
		{"`boxer`.`facts`", "boxer.facts", true, true},
		{`"boxer"."facts"`, "boxer.facts", true, true},
		{"`boxer`.facts", "boxer.facts", true, true},
		{`boxer."facts"`, "boxer.facts", true, true},
		{"  boxer.facts\n", "boxer.facts", true, true},
		{"facts", "facts", false, true},
		{"`facts`", "facts", false, true},
		{"Boxer.Facts", "Boxer.Facts", true, true},
		{"`my db`.`my table`", "my db.my table", true, true},
		{"", "", false, false},
		{"boxer.", "", false, false},
		{".facts", "", false, false},
		{"a.b.c", "", false, false},
		{"`boxer.facts", "", false, false},
		{"`bo\\`xer`.facts", "", false, false},
		{"boxer .facts", "", false, false},
		{"boxer.facts; DROP TABLE x", "", false, false},
		{"`boxer`x.facts", "", false, false},
	} {
		canon, qualified, ok := recordstore.CanonicalTableRef(c.ref)
		assert.Equal(t, c.ok, ok, "ok of %q", c.ref)
		assert.Equal(t, c.canon, canon, "canonical form of %q", c.ref)
		assert.Equal(t, c.qualified, qualified, "qualified of %q", c.ref)
	}
}

// Every spelling of the registered table is transformed, by the registry and
// by the one-table decorator alike; a bare name that could be the table is
// refused before anything is sent; other tables pass.
func TestInsertTransform_MatchesEverySpellingOfTheTable(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer alloc.AssertSize(t, 0)
	in := oneColumn(alloc, "id", 7)
	defer in.Release()

	unregister := recordstore.RegisterInsertTransform("scratch_spell.facts", renameTo(alloc, "scratch_spell.carrier"))
	defer unregister()
	assert.Equal(t, []string{"scratch_spell.facts"}, recordstore.RegisteredInsertTransformTables())

	var viaRegistry, viaOne []sentInsert
	for name, c := range map[string]struct {
		exec recordstore.ExecutorI
		sent *[]sentInsert
	}{
		"registry":  {recordstore.RegisteredTransforms(recorder(&viaRegistry)), &viaRegistry},
		"one table": {recordstore.TransformExecutor(recorder(&viaOne), "`scratch_spell`.facts", renameTo(alloc, "scratch_spell.carrier")), &viaOne},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			for _, spelling := range []string{"scratch_spell.facts", "`scratch_spell`.`facts`", `"scratch_spell"."facts"`, " scratch_spell.`facts` "} {
				require.NoError(t, c.exec.InsertArrow(ctx, spelling, []arrow.RecordBatch{in}), spelling)
			}
			require.Len(t, *c.sent, 4)
			for _, s := range *c.sent {
				assert.Equal(t, "scratch_spell.carrier", s.table)
				assert.Equal(t, []string{"carried"}, s.columns)
			}
			*c.sent = nil

			err := c.exec.InsertArrow(ctx, "facts", []arrow.RecordBatch{in})
			require.ErrorIs(t, err, recordstore.ErrUnqualifiedTransformedTable)
			err = c.exec.InsertArrow(ctx, "`facts`", []arrow.RecordBatch{in})
			require.ErrorIs(t, err, recordstore.ErrUnqualifiedTransformedTable)
			assert.Empty(t, *c.sent, "a bare name that could be the transformed table sends nothing")

			for _, other := range []string{"carrier", "other_db.facts", "scratch_spell.facts2", "Scratch_Spell.facts"} {
				require.NoError(t, c.exec.InsertArrow(ctx, other, []arrow.RecordBatch{in}), other)
			}
			require.Len(t, *c.sent, 4)
			for i, other := range []string{"carrier", "other_db.facts", "scratch_spell.facts2", "Scratch_Spell.facts"} {
				assert.Equal(t, other, (*c.sent)[i].table, "another table is sent as named")
				assert.Equal(t, []string{"id"}, (*c.sent)[i].columns)
			}
		})
	}

	_, ok := recordstore.RegisteredInsertTransform("`scratch_spell`.`facts`")
	assert.True(t, ok)
	_, ok = recordstore.RegisteredInsertTransform("facts")
	assert.False(t, ok, "a bare name has no registration")
	assert.Panics(t, func() { recordstore.RegisterInsertTransform("facts", renameTo(alloc, "x.y")) }, "a registration names its database")
	assert.Panics(t, func() { recordstore.TransformExecutor(recorder(&viaOne), "facts", renameTo(alloc, "x.y")) })
}

// A transform applies once per insert however many executors on the way
// consult it, including when its target is the registered table itself.
func TestInsertTransform_AppliesOnce(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer alloc.AssertSize(t, 0)
	in := oneColumn(alloc, "id", 7)
	defer in.Release()

	calls := 0
	inPlace := recordstore.InsertTransformFunc(func(table string, records []arrow.RecordBatch) (string, []arrow.RecordBatch, error) {
		calls++
		return renameTo(alloc, "scratch_once.facts")(table, records)
	})
	unregister := recordstore.RegisterInsertTransform("scratch_once.facts", inPlace)
	defer unregister()

	var sent []sentInsert
	exec := recordstore.RegisteredTransforms(recordstore.RegisteredTransforms(recorder(&sent)))
	require.NoError(t, exec.InsertArrow(context.Background(), "scratch_once.facts", []arrow.RecordBatch{in}))
	assert.Equal(t, 1, calls)
	require.Len(t, sent, 1)
	assert.Equal(t, []string{"carried"}, sent[0].columns)

	// By hand: the caller that transformed marks the context.
	target, out, release, err := recordstore.TransformInsert(inPlace, "scratch_once.facts", []arrow.RecordBatch{in})
	require.NoError(t, err)
	require.NoError(t, exec.InsertArrow(recordstore.WithTransformApplied(context.Background()), target, out))
	release()
	assert.Equal(t, 2, calls, "the marked insert was not transformed again")

	// ApplyRegisteredTransform, the consulting executors' call, honours it too.
	gotTarget, gotOut, rel, err := recordstore.ApplyRegisteredTransform(recordstore.WithTransformApplied(context.Background()), "scratch_once.facts", []arrow.RecordBatch{in})
	require.NoError(t, err)
	rel()
	assert.Equal(t, "scratch_once.facts", gotTarget)
	assert.Same(t, in, gotOut[0])
	assert.Equal(t, 2, calls)
}

// An observer above the transform is told the table the caller named and the
// table the rows went to.
func TestInsertTransform_ObserverLearnsTheTarget(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer alloc.AssertSize(t, 0)
	in := oneColumn(alloc, "id", 1, 2)
	defer in.Release()
	unregister := recordstore.RegisterInsertTransform("scratch_obs.facts", renameTo(alloc, "scratch_obs.carrier"))
	defer unregister()

	var events []recordstore.CallEvent
	var sent []sentInsert
	observer := recordstore.ObserverFunc(func(ev recordstore.CallEvent) error { events = append(events, ev); return nil })
	exec := recordstore.ObserveExecutor(recordstore.RegisteredTransforms(recorder(&sent)), observer)

	require.NoError(t, exec.InsertArrow(context.Background(), "scratch_obs.facts", []arrow.RecordBatch{in}))
	require.NoError(t, exec.InsertArrow(context.Background(), "scratch_obs.other", []arrow.RecordBatch{in}))
	require.Len(t, events, 2)
	assert.Equal(t, "scratch_obs.facts", events[0].Table)
	assert.Equal(t, "scratch_obs.carrier", events[0].Target)
	assert.EqualValues(t, 2, events[0].Rows)
	assert.Equal(t, "scratch_obs.other", events[1].Table)
	assert.Empty(t, events[1].Target, "an insert that was not transformed names no target")

	// Below the transform the observer sees the insert as it is sent.
	events = nil
	below := recordstore.RegisteredTransforms(recordstore.ObserveExecutor(recorder(&sent), observer))
	require.NoError(t, below.InsertArrow(context.Background(), "scratch_obs.facts", []arrow.RecordBatch{in}))
	require.Len(t, events, 1)
	assert.Equal(t, "scratch_obs.carrier", events[0].Table)
	assert.Empty(t, events[0].Target)
}

// inPlace is a transform that also answers in-place statements.
type inPlace struct {
	recordstore.InsertTransformFunc
	answer func(table string, stmt recordstore.InPlaceStatement) (string, error)
}

func (inst inPlace) TransformInPlace(table string, stmt recordstore.InPlaceStatement) (string, error) {
	return inst.answer(table, stmt)
}

// A statement that changes stored rows by a table's name runs as composed
// when nothing transforms the table, as the transform rewrites it when the
// transform can, and not at all otherwise.
func TestInPlaceStatementFor(t *testing.T) {
	alloc := memory.NewGoAllocator()
	stmt := recordstore.InPlaceStatement{Op: "test.op", SQL: "ALTER TABLE scratch_ip.facts DELETE WHERE 1", Args: map[string]string{"k": "v"}}

	sql, err := recordstore.InPlaceStatementFor("scratch_ip.facts", stmt)
	require.NoError(t, err)
	assert.Equal(t, stmt.SQL, sql, "no transform: the statement as composed")

	unregister := recordstore.RegisterInsertTransform("scratch_ip.facts", renameTo(alloc, "scratch_ip.carrier"))
	_, err = recordstore.InPlaceStatementFor("`scratch_ip`.`facts`", stmt)
	require.ErrorIs(t, err, recordstore.ErrTransformedTable, "a transform that maps inserts only leaves the statement refused")
	unregister()

	var seen recordstore.InPlaceStatement
	answer := "ALTER TABLE scratch_ip.carrier DELETE WHERE tenant = 'one'"
	var answerErr error
	unregister = recordstore.RegisterInsertTransform("scratch_ip.facts", inPlace{
		InsertTransformFunc: renameTo(alloc, "scratch_ip.carrier"),
		answer: func(table string, s recordstore.InPlaceStatement) (string, error) {
			seen = s
			return answer, answerErr
		},
	})
	defer unregister()
	sql, err = recordstore.InPlaceStatementFor("scratch_ip.facts", stmt)
	require.NoError(t, err)
	assert.Equal(t, answer, sql)
	assert.Equal(t, stmt, seen, "the transform is handed the op, the statement and its arguments")

	answer = "  "
	_, err = recordstore.InPlaceStatementFor("scratch_ip.facts", stmt)
	require.ErrorIs(t, err, recordstore.ErrTransformedTable, "no statement is a refusal, not a statement that does nothing")
	answer, answerErr = "", errors.New("this carrier keeps no text")
	_, err = recordstore.InPlaceStatementFor("scratch_ip.facts", stmt)
	require.ErrorContains(t, err, "keeps no text")
}
