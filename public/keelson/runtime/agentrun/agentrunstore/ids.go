package agentrunstore

import (
	"context"

	"github.com/stergiotis/boxer/public/identity/identgen/leased"
	"github.com/stergiotis/boxer/public/identity/identgen/leased/memalloc"
	"github.com/stergiotis/boxer/public/identity/identgen/leased/rowalloc"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// Ids are the journal's two id streams (ADR-0325 §SD5): Runs mints a run
// row's Key, Entries an entry's tagged id.
type Ids struct {
	Runs    identifier.IdGeneratorI
	Entries identifier.IdGeneratorI
}

// LeasedIds are the streams of the persisted journal: blocks leased
// through the lease table in the layout's database (ADR-0319 §SD3), which
// ProvisionIn creates. Building them leases nothing.
func LeasedIds(exec recordstore.ExecutorI, layout Layout) (ids Ids, err error) {
	alloc, err := rowalloc.NewAllocator(exec, rowalloc.Layout{Database: layout.Database})
	if err != nil {
		return ids, eh.Errorf("id lease allocator: %w", err)
	}
	if ids.Runs, err = rowalloc.NewSequence(alloc, vocab.IdStreamAgentRun, 0); err != nil {
		return ids, eh.Errorf("run id stream: %w", err)
	}
	if ids.Entries, err = rowalloc.NewSequence(alloc, vocab.IdStreamAgentRunEntry, 0); err != nil {
		return ids, eh.Errorf("entry id stream: %w", err)
	}
	return
}

// MemIds are the streams of a journal that lasts the process: counters
// under the in-memory claims, so an id minted here is never mistaken for
// a persisted one (ADR-0319 §SD6).
func MemIds() (ids Ids) {
	alloc := memalloc.NewAllocator()
	runs, err := leased.NewSequence(alloc, vocab.IdStreamAgentRunMem.Value(), rowalloc.DefaultBlockSize)
	if err != nil {
		panic(eh.Errorf("in-memory run id stream: %w", err))
	}
	entries, err := leased.NewSequence(alloc, vocab.IdStreamAgentRunEntryMem.Value(), rowalloc.DefaultBlockSize)
	if err != nil {
		panic(eh.Errorf("in-memory entry id stream: %w", err))
	}
	return Ids{Runs: runs, Entries: entries}
}

// Mint draws the next id from g.
func Mint(ctx context.Context, g identifier.IdGeneratorI) (id uint64, err error) {
	if g == nil {
		return 0, eh.Errorf("no id stream")
	}
	t, _, err := g.GetId(ctx, nil)
	if err != nil {
		return 0, eh.Errorf("mint: %w", err)
	}
	return t.Value(), nil
}
