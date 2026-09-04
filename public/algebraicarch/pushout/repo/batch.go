package repo

import (
	"bytes"
	"context"
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh/eb"

	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/pushoutgraph/types"
)

// BatchReport describes one ApplyEnvelopes run.
type BatchReport struct {
	// Applied lists the newly applied patches in the order they were
	// appended to the log: dependencies before dependents, ties in
	// bytewise hash order, independent of the order they were handed in.
	Applied []t.PatchHash
	// Duplicates lists envelopes that were already applied — before the
	// batch, or earlier within it — in input order.
	Duplicates []t.PatchHash
	// Pending lists envelopes the batch could not apply because a
	// dependency is applied neither in the repo nor by the batch. They
	// have no effect on the repo; resubmit them once the missing
	// patches have arrived.
	Pending []PendingEnvelope
}

// PendingEnvelope is one envelope ApplyEnvelopes had to leave out.
type PendingEnvelope struct {
	Hash t.PatchHash
	// Missing lists the direct dependencies still unapplied after the
	// batch — either absent altogether or themselves pending.
	Missing []t.PatchHash
}

// ApplyEnvelopes ingests a batch of framed envelopes in one transaction,
// accepting them in ANY order: the batch is sorted so dependencies
// precede dependents, whether the dependency is already applied or is
// another member of the batch. Envelopes whose dependencies are neither
// are reported as Pending rather than failing the batch (the per-
// envelope verb ApplyEnvelope rejects the same case with
// ErrMissingDependency). Duplicates are not errors.
//
// The applicable subset commits all-or-nothing in memory: one clone of
// the pushoutgraph takes every apply, then the envelopes are persisted,
// then the log entries are appended in the sorted order — via
// BatchAppenderI when the storage offers it — and only then does the
// clone become the repo state. An undecodable envelope or a patch the
// pushoutgraph rejects fails the batch before any write. A storage
// failure is crash-equivalent, as for every verb: what reached the log
// is a dependency-closed prefix, and Open reconciles.
//
// Cost is one clone plus one apply per patch, against one clone per
// patch through ApplyEnvelope — the difference between O(state + n)
// and O(state × n) for ingesting n patches.
func (inst *Repo) ApplyEnvelopes(ctx context.Context, framed [][]byte) (report BatchReport, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if err = inst.checkOpenLocked(); err != nil {
		return
	}

	// Decode everything up front; a malformed envelope fails the batch
	// before any mutation.
	type member struct {
		info    PatchInfo
		framed  []byte
		deps    []t.PatchHash // direct deps not already applied
		blocked bool          // some dep is neither applied nor in the batch
	}
	members := make(map[t.PatchHash]*member, len(framed))
	for _, fr := range framed {
		env, codecName, derr := inst.reg.Decode(fr)
		if derr != nil {
			err = derr
			return
		}
		h := env.Patch.Hash
		if _, dup := inst.appliedSet[h]; dup {
			report.Duplicates = append(report.Duplicates, h)
			continue
		}
		if _, dup := members[h]; dup {
			report.Duplicates = append(report.Duplicates, h)
			continue
		}
		m := &member{
			info:   PatchInfo{Patch: env.Patch, Producer: env.Producer, Timestamp: env.Timestamp, Codec: codecName},
			framed: slices.Clone(fr),
		}
		for _, dep := range env.Patch.Dependencies {
			if _, ok := inst.appliedSet[dep]; !ok {
				m.deps = append(m.deps, dep)
			}
		}
		members[h] = m
	}

	// Kahn's algorithm over the in-batch dependency edges. A member
	// whose unapplied dependency is not in the batch never becomes
	// ready; neither does anything downstream of it. The ready set is
	// drained in bytewise hash order so the log order is a function of
	// the set, not of the input order.
	indegree := make(map[t.PatchHash]int, len(members))
	dependents := make(map[t.PatchHash][]t.PatchHash)
	var ready []t.PatchHash
	for h, m := range members {
		for _, dep := range m.deps {
			if _, inBatch := members[dep]; inBatch {
				indegree[h]++
				dependents[dep] = append(dependents[dep], h)
			} else {
				m.blocked = true
			}
		}
		if !m.blocked && indegree[h] == 0 {
			ready = append(ready, h)
		}
	}
	less := func(a, b t.PatchHash) int { return bytes.Compare(a[:], b[:]) }
	slices.SortFunc(ready, less)
	order := make([]t.PatchHash, 0, len(members))
	done := make(map[t.PatchHash]struct{}, len(members))
	for len(ready) > 0 {
		h := ready[0]
		ready = ready[1:]
		order = append(order, h)
		done[h] = struct{}{}
		for _, d := range dependents[h] {
			indegree[d]--
			if indegree[d] == 0 && !members[d].blocked {
				i, _ := slices.BinarySearchFunc(ready, d, less)
				ready = slices.Insert(ready, i, d)
			}
		}
	}
	for h, m := range members {
		if _, ok := done[h]; ok {
			continue
		}
		pe := PendingEnvelope{Hash: h}
		for _, dep := range m.deps {
			if _, ok := done[dep]; !ok {
				pe.Missing = append(pe.Missing, dep)
			}
		}
		slices.SortFunc(pe.Missing, less)
		report.Pending = append(report.Pending, pe)
	}
	slices.SortFunc(report.Pending, func(a, b PendingEnvelope) int { return less(a.Hash, b.Hash) })
	if len(order) == 0 {
		return
	}

	// Transactional tail, batched: one clone, n applies, n envelope
	// writes, one retention save, the log appends, one in-memory swap.
	next := inst.g.Clone()
	tombstones := false
	for _, h := range order {
		p := members[h].info.Patch
		if aerr := p.Apply(next); aerr != nil {
			err = eb.Build().Stringer("hash", h).Errorf("apply: %w", aerr)
			return
		}
		tombstones = tombstones || patchTombstones(p)
	}
	for _, h := range order {
		if err = ctx.Err(); err != nil {
			return
		}
		if err = inst.st.PutEnvelope(ctx, h, members[h].framed); err != nil {
			return
		}
	}
	if tombstones {
		if err = inst.saveRetentionLocked(ctx, next); err != nil {
			return
		}
	}
	if ba, ok := inst.st.(BatchAppenderI); ok {
		err = ba.AppendAppliedBatch(ctx, order)
	} else {
		for _, h := range order {
			if err = inst.st.AppendApplied(ctx, h); err != nil {
				break
			}
		}
	}
	if err != nil {
		return
	}
	inst.g = next
	inst.applied = append(inst.applied, order...)
	for _, h := range order {
		inst.appliedSet[h] = struct{}{}
	}
	inst.metaMu.Lock()
	for _, h := range order {
		inst.meta[h] = members[h].info
	}
	inst.metaMu.Unlock()
	report.Applied = order
	if inst.hooks.OnApplied != nil {
		for _, h := range order {
			inst.hooks.OnApplied(AppliedEvent{Hash: h, Producer: members[h].info.Producer, NewlyRecorded: false})
		}
	}
	return
}
