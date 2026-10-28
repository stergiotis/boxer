package coverage

import (
	"bytes"
	"runtime/coverage"

	"github.com/RoaringBitmap/roaring"
	"github.com/stergiotis/boxer/public/observability/coverage/covsnap"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ReadCounters snapshots the process's live counters into buf and decodes
// them (ADR-0343 §SD5). It is the side-effect-free read a bracket takes
// twice; unlike Sampler.Sample it folds nothing and consumes nothing from
// the published stream, so any number of readers may call it, each with a
// buffer of its own. It fails on a binary without -cover -covermode=atomic.
func ReadCounters(buf *bytes.Buffer) (snap *covsnap.CounterSnapshot, err error) {
	buf.Reset()
	err = coverage.WriteCounters(buf)
	if err != nil {
		return nil, eh.Errorf("unable to snapshot coverage counters: %w", err)
	}
	snap, err = DecodeCounters(buf.Bytes())
	if err != nil {
		return nil, eh.Errorf("unable to decode this binary's coverage counters: %w", err)
	}
	return
}

// Diff is what moved between two counter snapshots of one build.
type Diff struct {
	// Units holds the global unit ids (ADR-0169 §SD3) whose counter differs
	// between the two snapshots.
	Units *roaring.Bitmap
	// Funcs counts the functions that own at least one changed unit.
	Funcs uint32
}

// DiffCounters returns the units whose counter changed from before to after
// (ADR-0343 §SD5). It is the attribution primitive: counters are monotone,
// so a unit whose counter moved between two reads executed between them,
// whether or not it had executed before — which is what the accumulator's
// 0→1 fold cannot say. A function absent from before is one that had not
// executed; every non-zero counter of its after entry counts as changed.
// Both snapshots must be of meta's build.
func DiffCounters(meta *covsnap.MetaProfile, before *covsnap.CounterSnapshot, after *covsnap.CounterSnapshot) (d Diff, err error) {
	if before.MetaHash != meta.Hash || after.MetaHash != meta.Hash {
		return Diff{}, eb.Build().Hex("beforeMetaHash", before.MetaHash[:]).Hex("afterMetaHash", after.MetaHash[:]).Hex("buildMetaHash", meta.Hash[:]).Errorf("counter snapshots do not belong to the build")
	}
	prev := make(map[[2]uint32][]uint32, len(before.Funcs))
	for _, fc := range before.Funcs {
		prev[[2]uint32{fc.PkgIdx, fc.FuncIdx}] = fc.Counters
	}
	d.Units = roaring.New()
	for _, fc := range after.Funcs {
		_, fn, ok := meta.LookupFunc(fc.PkgIdx, fc.FuncIdx)
		if !ok {
			return Diff{}, eb.Build().Uint32("pkgIdx", fc.PkgIdx).Uint32("funcIdx", fc.FuncIdx).Errorf("counter snapshot references an unknown function")
		}
		if len(fc.Counters) != len(fn.Units) {
			return Diff{}, eb.Build().Int("counters", len(fc.Counters)).Uint32("pkgIdx", fc.PkgIdx).Uint32("funcIdx", fc.FuncIdx).Int("units", len(fn.Units)).Errorf("counter snapshot has a different number of counters than the function has units")
		}
		was := prev[[2]uint32{fc.PkgIdx, fc.FuncIdx}]
		touched := false
		for i, c := range fc.Counters {
			if i < len(was) && was[i] == c {
				continue
			}
			if i >= len(was) && c == 0 {
				continue
			}
			d.Units.Add(fn.UnitBase + uint32(i))
			touched = true
		}
		if touched {
			d.Funcs++
		}
	}
	return
}
