package anchorstore

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
)

// The service appends heads and reads them back by run over the bus; the
// receipt names the sender.
func TestTheServiceAppendsAndReadsHeads(t *testing.T) {
	ctx := context.Background()
	bus := inprocbus.NewInst(zerolog.Nop())
	store := NewMemStore()
	svc, err := NewService(bus, zerolog.Nop(), store)
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	cli := NewClient(bus.NewClient("app.chat", ClientCaps("test")))

	r, err := cli.Append(ctx, Head{Cell: "c", Run: "r1", Ordinal: 3, Digest: "abc"})
	require.NoError(t, err)
	assert.NotEmpty(t, r.Id)
	assert.Equal(t, "app.chat", r.By)
	assert.False(t, r.Durable, "the memory store anchors nothing beyond the process")
	_, err = cli.Append(ctx, Head{Run: "r1"})
	assert.Error(t, err, "a head names its digest")
	same, err := cli.Append(ctx, Head{Cell: "c", Run: "r1", Ordinal: 3, Digest: "abc"})
	require.NoError(t, err)
	assert.Equal(t, r.Id, same.Id, "the same head again is its first receipt")
	_, err = cli.Append(ctx, Head{Cell: "c", Run: "r1", Ordinal: 3, Digest: "xyz"})
	assert.Error(t, err, "another digest at an anchored ordinal is refused")

	heads, err := cli.Read(ctx, "c", "r1")
	require.NoError(t, err)
	require.Len(t, heads, 1)
	assert.Equal(t, uint32(3), heads[0].Ordinal)
	assert.Equal(t, r.Id, heads[0].Receipt.Id)
	heads, err = cli.Read(ctx, "c", "other")
	require.NoError(t, err)
	assert.Empty(t, heads)
}

// Law L32: heads shipped from a chained journal verify against the store;
// an entry tampered after anchoring breaks every head past it, and a
// digest the store does not hold breaks its head.
func TestLawL32(t *testing.T) {
	ctx := context.Background()
	journal := agentrun.NewMemStore()
	anchors := NewMemStore()
	w, err := agentrun.Open(ctx, journal, agentrun.Run{Id: "r1", Cell: "c"})
	require.NoError(t, err)
	_, err = w.Append(ctx, agentrun.Entry{Kind: agentrun.KindUser, Body: "hello", Turn: "t1"}, agentrun.Entry{Kind: agentrun.KindTurn, Turn: "t1", Body: "answered"})
	require.NoError(t, err)
	head1, err := Ship(ctx, anchors, w, DefaultCadence())
	require.NoError(t, err)
	assert.Equal(t, agentrun.KindHead, head1.Kind)
	assert.Equal(t, uint32(2), head1.Target)
	rec, err := ParseHeadBody(head1.Body)
	require.NoError(t, err)
	assert.Equal(t, "turn,close", rec.Cadence)
	_, err = w.Append(ctx, agentrun.Entry{Kind: agentrun.KindUser, Body: "more", Turn: "t2"})
	require.NoError(t, err)
	_, err = Ship(ctx, anchors, w, DefaultCadence())
	require.NoError(t, err)

	anchored, err := anchors.Read(ctx, "c", "r1")
	require.NoError(t, err)
	require.Len(t, anchored, 2)
	entries := w.Entries()
	assert.Empty(t, VerifyHeads(w.Run().Digest, entries, anchored))

	tampered := append([]agentrun.Entry(nil), entries...)
	tampered[0].Body = "hullo"
	broken := VerifyHeads(w.Run().Digest, tampered, anchored)
	last := entries[len(entries)-1].Ordinal
	assert.Equal(t, []uint32{anchored[0].Ordinal, head1.Ordinal, anchored[1].Ordinal, last}, broken,
		"both heads cover the tampered entry, and neither anchored head is reproduced")

	tampered = append([]agentrun.Entry(nil), entries...)
	tampered[3].Body = "mire"
	broken = VerifyHeads(w.Run().Digest, tampered, anchored)
	assert.Equal(t, []uint32{anchored[1].Ordinal, last}, broken, "the first head stands; the second covers the change")

	forged := append([]Anchored(nil), anchored...)
	forged[0].Digest = "not what was anchored"
	assert.Equal(t, []uint32{anchored[0].Ordinal, head1.Ordinal}, VerifyHeads(w.Run().Digest, entries, forged),
		"the head entry is not what the store holds, and the store's head is not what the chain reproduces")
	assert.Empty(t, VerifyHeads(w.Run().Digest, nil, nil))
	rec, err = ParseHeadBody(head1.Body)
	require.NoError(t, err)
	assert.False(t, rec.Durable, "the head entry records that the anchor is the process's alone")
}

// L32 in the other direction: a journal rewritten from the start and
// re-chained, with its head entries dropped, breaks against the store's
// heads; a tail re-anchored after the fact breaks at the ordinal whose
// first receipt disagrees.
func TestLawL32HoldsAgainstDroppedHeadsAndReanchoring(t *testing.T) {
	ctx := context.Background()
	journal := agentrun.NewMemStore()
	anchors := NewMemStore()
	w, err := agentrun.Open(ctx, journal, agentrun.Run{Id: "r1", Cell: "c"})
	require.NoError(t, err)
	_, err = w.Append(ctx, agentrun.Entry{Kind: agentrun.KindUser, Body: "hello", Turn: "t1"}, agentrun.Entry{Kind: agentrun.KindTurn, Turn: "t1", Body: "answered"})
	require.NoError(t, err)
	head, err := Ship(ctx, anchors, w, DefaultCadence())
	require.NoError(t, err)
	anchored, err := anchors.Read(ctx, "c", "r1")
	require.NoError(t, err)

	// The adversary rewrites entry 1, recomputes the chain and drops the
	// head entry: the journal verifies on its own (L31) and not against
	// the store (L32).
	rewritten := agentrun.NewMemStore()
	w2, err := agentrun.Open(ctx, rewritten, w.Run())
	require.NoError(t, err)
	_, err = w2.Append(ctx, agentrun.Entry{Kind: agentrun.KindUser, Body: "hullo", Turn: "t1"}, agentrun.Entry{Kind: agentrun.KindTurn, Turn: "t1", Body: "answered"})
	require.NoError(t, err)
	broken, err := agentrun.Verify(w2.Run().Digest, w2.Entries())
	require.NoError(t, err)
	assert.Zero(t, broken, "the rewritten chain holds on its own")
	assert.Equal(t, []uint32{head.Target}, VerifyHeads(w2.Run().Digest, w2.Entries(), anchored), "and breaks against the anchored head")

	// Re-anchoring the rewritten head does not help: the first receipt
	// for the ordinal counts, and the second disagrees.
	ord, digest := w2.Head()
	_, err = anchors.Append(ctx, Head{Cell: "c", Run: "r1", Ordinal: ord, Digest: digest})
	assert.Error(t, err, "the store refuses another digest at the ordinal")
	conflicting := append([]Anchored(nil), anchored...)
	conflicting = append(conflicting, Anchored{Head: Head{Cell: "c", Run: "r1", Ordinal: ord, Digest: digest}, Receipt: Receipt{Id: "anchor-late", At: Now()}})
	assert.Equal(t, []uint32{ord}, VerifyHeads(w2.Run().Digest, w2.Entries(), conflicting))
	assert.Equal(t, []uint32{ord}, VerifyHeads(w.Run().Digest, w.Entries(), conflicting), "the conflict breaks the ordinal for the true journal too")

	// A head entry alone, with the store empty, breaks: nothing anchors it.
	assert.Equal(t, []uint32{head.Ordinal}, VerifyHeads(w.Run().Digest, w.Entries(), nil))
}

// A deleted run's heads still verify (law L32 after ADR-0308 §SD8): the
// tombstone empties bodies, and a head entry's body is a digest, kept.
func TestLawL32HoldsAfterDeletion(t *testing.T) {
	ctx := context.Background()
	journal := agentrun.NewMemStore()
	anchors := NewMemStore()
	w, err := agentrun.Open(ctx, journal, agentrun.Run{Id: "r1", Cell: "c"})
	require.NoError(t, err)
	_, err = w.Append(ctx, agentrun.Entry{Kind: agentrun.KindUser, Body: "hello", Turn: "t1"}, agentrun.Entry{Kind: agentrun.KindTurn, Turn: "t1", Body: "answered"})
	require.NoError(t, err)
	_, err = Ship(ctx, anchors, w, DefaultCadence())
	require.NoError(t, err)
	require.NoError(t, w.Delete(ctx, "person"))
	entries, err := journal.Entries(ctx, "r1", 0)
	require.NoError(t, err)
	assert.Empty(t, entries[0].Body)
	assert.NotEmpty(t, entries[2].Body, "the head entry keeps its record")
	heads, err := anchors.Read(ctx, "c", "r1")
	require.NoError(t, err)
	assert.Empty(t, VerifyHeads(w.Run().Digest, entries, heads))
	again, err := agentrun.Reopen(ctx, journal, "r1")
	require.NoError(t, err)
	assert.Empty(t, VerifyHeads(again.Run().Digest, again.Entries(), heads))
}

// The service is scoped to the run's owner: without a resolver the first
// sender to anchor a run owns its heads, and another sender neither reads
// nor pre-anchors them; with the journal store resolving owners, the
// run row's owner app is the one.
func TestTheServiceScopesHeadsToTheRunsOwner(t *testing.T) {
	ctx := context.Background()
	bus := inprocbus.NewInst(zerolog.Nop())
	svc, err := NewService(bus, zerolog.Nop(), NewMemStore())
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	chat := NewClient(bus.NewClient("app.chat", ClientCaps("test")))
	other := NewClient(bus.NewClient("app.other", ClientCaps("test")))
	_, err = chat.Append(ctx, Head{Cell: "c", Run: "r1", Ordinal: 1, Digest: "d1"})
	require.NoError(t, err)
	_, err = other.Append(ctx, Head{Cell: "c", Run: "r1", Ordinal: 2, Digest: "d2"})
	assert.Error(t, err, "another app does not anchor the run")
	_, err = other.Read(ctx, "c", "r1")
	assert.Error(t, err, "nor reads its heads")
	heads, err := chat.Read(ctx, "c", "r1")
	require.NoError(t, err)
	assert.Len(t, heads, 1)

	journal := agentrun.NewMemStore()
	require.NoError(t, journal.CreateRun(ctx, agentrun.Run{Id: "r2", OwnerApp: "app.chat"}))
	svc.Owners = RunOwners(journal)
	_, err = other.Append(ctx, Head{Run: "r2", Ordinal: 1, Digest: "d"})
	assert.Error(t, err, "the journal names another owner")
	_, err = other.Append(ctx, Head{Run: "r9", Ordinal: 1, Digest: "d"})
	assert.Error(t, err, "a run the journal does not hold is no one's")
	_, err = chat.Append(ctx, Head{Run: "r2", Ordinal: 1, Digest: "d"})
	require.NoError(t, err)
	heads, err = chat.Read(ctx, "", "r2")
	require.NoError(t, err)
	assert.Len(t, heads, 1)
}
