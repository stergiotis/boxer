package recordstore

import (
	"context"
	"iter"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInsertSettingsRender(t *testing.T) {
	assert.True(t, InsertSettings{}.IsZero())
	assert.Equal(t, "", InsertSettings{}.SQL())
	assert.Nil(t, InsertSettings{}.Map())
	s := InsertSettings{AsyncInsert: ToggleOff, WaitForAsyncInsert: ToggleOn}
	assert.Equal(t, " SETTINGS async_insert=0, wait_for_async_insert=1", s.SQL())
	assert.Equal(t, map[string]string{"async_insert": "0", "wait_for_async_insert": "1"}, s.Map())
}

// plainExec carries no per-insert settings; withExec does, and records
// what it was given.
type plainExec struct{ inserts int }

func (inst *plainExec) Exec(context.Context, string) error { return nil }
func (inst *plainExec) QueryArrow(context.Context, string) iter.Seq2[arrow.RecordBatch, error] {
	return func(func(arrow.RecordBatch, error) bool) {}
}
func (inst *plainExec) InsertArrow(context.Context, string, []arrow.RecordBatch) error {
	inst.inserts++
	return nil
}

type withExec struct {
	plainExec
	got InsertSettings
}

func (inst *withExec) InsertArrowWith(_ context.Context, _ string, _ []arrow.RecordBatch, s InsertSettings) (bool, error) {
	inst.got = s
	return true, nil
}

// The helper uses the seam when the executor has it and says when it did
// not; an observing wrapper forwards the seam rather than hiding it.
func TestInsertArrowWithUsesTheSeamAndReportsItsAbsence(t *testing.T) {
	ctx := context.Background()
	s := InsertSettings{AsyncInsert: ToggleOff}

	plain := &plainExec{}
	applied, err := InsertArrowWith(ctx, plain, "t", nil, s)
	require.NoError(t, err)
	assert.False(t, applied)
	assert.Equal(t, 1, plain.inserts)

	with := &withExec{}
	applied, err = InsertArrowWith(ctx, with, "t", nil, s)
	require.NoError(t, err)
	assert.True(t, applied)
	assert.Equal(t, s, with.got)

	applied, err = InsertArrowWith(ctx, plain, "t", nil, InsertSettings{})
	require.NoError(t, err)
	assert.True(t, applied, "settings that set nothing are always applied")

	observed := ObserveExecutor(&withExec{})
	applied, err = InsertArrowWith(ctx, observed, "t", nil, s)
	require.NoError(t, err)
	assert.True(t, applied, "the observing wrapper forwards the seam")
	applied, err = InsertArrowWith(ctx, ObserveExecutor(&plainExec{}), "t", nil, s)
	require.NoError(t, err)
	assert.False(t, applied, "and reports its absence underneath")
}
