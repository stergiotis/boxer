package rowintern

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/identity/identgen"
	"github.com/stergiotis/boxer/public/identity/identgen/leased"
	"github.com/stergiotis/boxer/public/identity/identgen/leased/memalloc"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

const testTag identifier.TagValue = 317811

func localExec(t *testing.T) (exec *chexec.LocalExecutor) {
	t.Helper()
	exec, err := chexec.NewLocalExecutor(filepath.Join(t.TempDir(), "chdb"), memory.NewGoAllocator())
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}
	return
}

// sequence is a sequential generator of the test stream over alloc, as a
// process would hold one.
func sequence(t *testing.T, alloc identgen.AllocatorI) identifier.IdGeneratorI {
	t.Helper()
	seq, err := leased.NewSequence(alloc, testTag, 4)
	require.NoError(t, err)
	return seq
}

// The same key resolves to the same id from two generators that share
// nothing but the directory and the stream; another key and another scope
// get ids of their own; and a resolved key costs the server nothing again.
func TestTheSameKeyResolvesToTheSameIdFromEveryInstance(t *testing.T) {
	ctx := context.Background()
	exec := localExec(t)
	layout := Layout{Database: "interntest"}
	require.NoError(t, ProvisionIn(ctx, exec, layout))
	require.NoError(t, ProvisionIn(ctx, exec, layout), "provisioning is idempotent")
	alloc := memalloc.NewAllocator()

	a, err := New(exec, layout, "series", sequence(t, alloc))
	require.NoError(t, err)
	b, err := New(exec, layout, "series", sequence(t, alloc))
	require.NoError(t, err)
	other, err := New(exec, layout, "runs", sequence(t, alloc))
	require.NoError(t, err)

	id1, fresh, err := a.GetId(ctx, []byte("host/cpu"))
	require.NoError(t, err)
	assert.True(t, fresh)
	assert.Equal(t, testTag, id1.GetTag().GetValue())
	assert.NotZero(t, id1.RemoveTag())

	id2, fresh, err := b.GetId(ctx, []byte("host/cpu"))
	require.NoError(t, err)
	assert.False(t, fresh, "the key had its id")
	assert.Equal(t, id1, id2, "another process's generator resolves the key to the same id")

	again, fresh, err := a.GetId(ctx, []byte("host/cpu"))
	require.NoError(t, err)
	assert.False(t, fresh)
	assert.Equal(t, id1, again)

	mem, _, err := a.GetId(ctx, []byte("host/mem"))
	require.NoError(t, err)
	assert.NotEqual(t, id1, mem)

	scoped, fresh, err := other.GetId(ctx, []byte("host/cpu"))
	require.NoError(t, err)
	assert.True(t, fresh, "a key is unique within its scope")
	assert.NotEqual(t, id1, scoped)

	// A key that is not text.
	raw := []byte{0x00, 0xff, '\'', '\\', 0x80}
	r1, _, err := a.GetId(ctx, raw)
	require.NoError(t, err)
	r2, _, err := b.GetId(ctx, raw)
	require.NoError(t, err)
	assert.Equal(t, r1, r2)
}

// A reader looks a key up and never gives it an id.
func TestLookupDoesNotMint(t *testing.T) {
	ctx := context.Background()
	exec := localExec(t)
	layout := Layout{Database: "interntest"}
	require.NoError(t, ProvisionIn(ctx, exec, layout))
	alloc := memalloc.NewAllocator()
	writer, err := New(exec, layout, "series", sequence(t, alloc))
	require.NoError(t, err)
	reader, err := New(exec, layout, "series", sequence(t, alloc))
	require.NoError(t, err)

	_, found, err := reader.Lookup(ctx, []byte("unseen"))
	require.NoError(t, err)
	assert.False(t, found)
	want, fresh, err := writer.GetId(ctx, []byte("unseen"))
	require.NoError(t, err)
	assert.True(t, fresh, "the lookup left the key without an id")
	got, found, err := reader.Lookup(ctx, []byte("unseen"))
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, want, got)
	_, _, err = reader.Lookup(ctx, nil)
	require.Error(t, err)
}

// A batch resolves in order; a key named twice is resolved once and is
// fresh once; known keys cost nothing.
func TestGetIdsResolvesABatch(t *testing.T) {
	ctx := context.Background()
	exec := localExec(t)
	layout := Layout{Database: "interntest"}
	require.NoError(t, ProvisionIn(ctx, exec, layout))
	alloc := memalloc.NewAllocator()
	a, err := New(exec, layout, "corpus", sequence(t, alloc))
	require.NoError(t, err)
	known, _, err := a.GetId(ctx, []byte("known"))
	require.NoError(t, err)

	keys := [][]byte{[]byte("x"), []byte("known"), []byte("y"), []byte("x")}
	ids, fresh, err := a.GetIds(ctx, keys)
	require.NoError(t, err)
	require.Len(t, ids, 4)
	assert.Equal(t, known, ids[1])
	assert.Equal(t, ids[0], ids[3])
	assert.NotEqual(t, ids[0], ids[2])
	assert.Equal(t, []bool{true, false, true, false}, fresh)

	b, err := New(exec, layout, "corpus", sequence(t, alloc))
	require.NoError(t, err)
	fromB, freshB, err := b.GetIds(ctx, keys)
	require.NoError(t, err)
	assert.Equal(t, ids, fromB)
	assert.Equal(t, []bool{false, false, false, false}, freshB)

	_, _, err = a.GetIds(ctx, [][]byte{[]byte("z"), nil})
	require.Error(t, err, "an empty key is refused before anything is resolved")
	_, found, err := b.Lookup(ctx, []byte("z"))
	require.NoError(t, err)
	assert.False(t, found)
}

// The mapping is the row inserted first: a row for the same key that lands
// later, under another id, changes nothing for anyone.
func TestALaterRowForAKeyIsNotItsMapping(t *testing.T) {
	ctx := context.Background()
	exec := localExec(t)
	layout := Layout{Database: "interntest"}
	require.NoError(t, ProvisionIn(ctx, exec, layout))
	alloc := memalloc.NewAllocator()
	a, err := New(exec, layout, "series", sequence(t, alloc))
	require.NoError(t, err)
	first, _, err := a.GetId(ctx, []byte("k"))
	require.NoError(t, err)

	late := identifier.TagValue(testTag).GetTag().ComposeId(999_999)
	require.NoError(t, exec.Exec(ctx, "INSERT INTO interntest.idkey (tag, scope, key, id) VALUES (317811, 'series', 'k', "+
		"toUInt64("+uintLit(late.Value())+"))"))
	b, err := New(exec, layout, "series", sequence(t, alloc))
	require.NoError(t, err)
	got, fresh, err := b.GetId(ctx, []byte("k"))
	require.NoError(t, err)
	assert.False(t, fresh)
	assert.Equal(t, first, got)
}

// localOnly hands out ids under a tag that is not the one it reports: what
// a generator that falls back to process-local ids does.
type localOnly struct{ identifier.IdGeneratorI }

func (inst localOnly) GetId(ctx context.Context, k []byte) (identifier.TaggedId, bool, error) {
	return identifier.TagValue(53133).GetTag().ComposeId(7), true, nil
}

func TestRefusals(t *testing.T) {
	ctx := context.Background()
	exec := localExec(t)
	layout := Layout{Database: "interntest"}
	require.NoError(t, ProvisionIn(ctx, exec, layout))
	seq := sequence(t, memalloc.NewAllocator())

	_, err := New(nil, layout, "s", seq)
	require.Error(t, err)
	_, err = New(exec, layout, "s", nil)
	require.Error(t, err)
	_, err = New(exec, layout, "it's", seq)
	require.Error(t, err, "a scope is a name, not text spliced into SQL")
	_, err = New(exec, Layout{Database: "a;b"}, "s", seq)
	require.Error(t, err)

	bad, err := New(exec, layout, "s", localOnly{IdGeneratorI: seq})
	require.NoError(t, err)
	_, _, err = bad.GetId(ctx, []byte("k"))
	require.ErrorContains(t, err, "another tag", "an id unique only within a process never enters the directory")
	_, found, err := bad.Lookup(ctx, []byte("k"))
	require.NoError(t, err)
	assert.False(t, found, "and nothing was recorded")
}
