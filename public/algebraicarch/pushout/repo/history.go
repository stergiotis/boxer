package repo

import (
	"context"
	"slices"
	"sync"

	"github.com/stergiotis/boxer/public/observability/eh/eb"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/envelope"
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/pushoutgraph/types"
)

// history is the engine's access to decoded patch metadata (ADR-0221).
//
// Contract, which is all a consumer of [Repo.PatchInfo], [ViewI.PatchInfo]
// and [Repo.Unrecord] may rely on: the envelopes in storage are the
// system of record; the engine holds SOME subset of them decoded in
// memory, chosen by this component; a lookup may read and decode from
// storage, so it may fail with a storage error and is not O(1).
//
// This implementation keeps everything it has ever decoded — every
// patch replayed at Open (snapshot-covered history is not decoded) and
// every patch recorded or applied since — so a repo without snapshots
// holds its whole decoded history for the life of the handle. A bounded
// or lazy implementation (an LRU, a reverse-dependency index kept in
// storage, a store-side query for dependents) replaces this type
// without touching Repo's API: remember may drop, lookup may fetch, and
// dependents may answer from an index instead of a scan. The batch read
// verb (StorageI.LoadEnvelopes) is what makes the fetch path one round
// trip rather than one per patch.
type history struct {
	mu   sync.Mutex
	st   StorageI
	reg  *envelope.Registry
	meta map[t.PatchHash]PatchInfo
}

func newHistory(st StorageI, reg *envelope.Registry) *history {
	return &history{st: st, reg: reg, meta: make(map[t.PatchHash]PatchInfo)}
}

// remember offers a decoded patch to the cache. It may be dropped.
func (inst *history) remember(h t.PatchHash, info PatchInfo) {
	inst.mu.Lock()
	inst.meta[h] = info
	inst.mu.Unlock()
}

// lookup returns the logical envelope for h, decoding from storage on
// a miss and checking that the stored envelope carries the patch it is
// filed under. Callable under either engine lock.
func (inst *history) lookup(ctx context.Context, h t.PatchHash) (info PatchInfo, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if cached, ok := inst.meta[h]; ok {
		info = cached
		return
	}
	framed, err := inst.st.GetEnvelope(ctx, h)
	if err != nil {
		return
	}
	info, err = inst.decode(h, framed)
	if err != nil {
		return
	}
	inst.meta[h] = info
	return
}

// dependents returns, in log order, every hash of applied (other than
// target) whose patch declares target as a direct dependency. Patches
// not yet decoded are fetched in one LoadEnvelopes round trip.
func (inst *history) dependents(ctx context.Context, target t.PatchHash, applied []t.PatchHash) (out []t.PatchHash, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	var missing []t.PatchHash
	for _, h := range applied {
		if h == target {
			continue
		}
		if _, ok := inst.meta[h]; !ok {
			missing = append(missing, h)
		}
	}
	if len(missing) > 0 {
		for env, lerr := range inst.st.LoadEnvelopes(ctx, missing) {
			if lerr != nil {
				err = lerr
				return
			}
			var info PatchInfo
			info, err = inst.decode(env.Hash, env.Framed)
			if err != nil {
				return
			}
			inst.meta[env.Hash] = info
		}
	}
	for _, h := range applied {
		if h == target {
			continue
		}
		info, ok := inst.meta[h]
		if !ok {
			err = eb.Build().Stringer("patchHash", h).Errorf("store yielded no envelope for an applied patch: %w", ErrCorruptStore)
			return
		}
		if slices.Contains(info.Patch.Dependencies, target) {
			out = append(out, h)
		}
	}
	return
}

// decode turns framed bytes filed under h into a PatchInfo, refusing an
// envelope whose patch is not h.
func (inst *history) decode(h t.PatchHash, framed []byte) (info PatchInfo, err error) {
	env, codecName, err := inst.reg.Decode(framed)
	if err != nil {
		return
	}
	if env.Patch.Hash != h {
		err = eb.Build().Stringer("patchHash", h).Stringer("hash", env.Patch.Hash).Errorf("the stored envelope carries a different patch than the key it is filed under: %w", ErrCorruptStore)
		return
	}
	info = PatchInfo{Patch: env.Patch, Producer: env.Producer, Timestamp: env.Timestamp, Codec: codecName}
	return
}
