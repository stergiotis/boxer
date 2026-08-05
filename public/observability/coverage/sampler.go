package coverage

import (
	"bytes"
	"runtime/coverage"
	"time"

	"github.com/RoaringBitmap/roaring"
	"github.com/stergiotis/boxer/public/observability/coverage/covsnap"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// SamplerOptions parameterize the live sampler.
type SamplerOptions struct {
	// RestateEvery is forwarded to the fold engine; see AccumulatorOptions.
	RestateEvery uint64
}

// Sampler acquires periodic coverage updates from the running process:
// WriteCounters → decode → Accumulator.Fold (ADR-0169 §SD3/§SD4). It is
// the BundleSampler analog of the sysmetrics pipeline — the bus producer
// (M3) owns the ticker and calls Sample; construction fails cleanly on a
// binary without -cover -covermode=atomic, and the caller idles.
//
// Sample has a single-caller contract (the producer goroutine); Meta,
// MetaBlob, Status and CoveredBitmap are safe for concurrent readers.
type Sampler struct {
	acc      *Accumulator
	metaBlob []byte
	buf      bytes.Buffer
}

func NewSampler(opts SamplerOptions) (inst *Sampler, err error) {
	err = ProbeRuntimeSupport()
	if err != nil {
		return nil, eh.Errorf("coverage sampling unavailable (build with -cover -covermode=atomic): %w", err)
	}
	var mbuf bytes.Buffer
	err = coverage.WriteMeta(&mbuf)
	if err != nil {
		return nil, eh.Errorf("unable to snapshot coverage meta-data: %w", err)
	}
	var meta *covsnap.MetaProfile
	meta, err = DecodeMeta(mbuf.Bytes())
	if err != nil {
		return nil, eh.Errorf("unable to decode this binary's coverage meta-data: %w", err)
	}
	inst = &Sampler{
		acc:      NewAccumulator(meta, AccumulatorOptions{RestateEvery: opts.RestateEvery}),
		metaBlob: mbuf.Bytes(),
	}
	return
}

// Sample snapshots the live counters and folds them into one update.
func (inst *Sampler) Sample() (upd *covsnap.Update, err error) {
	inst.buf.Reset()
	err = coverage.WriteCounters(&inst.buf)
	if err != nil {
		return nil, eh.Errorf("unable to snapshot coverage counters: %w", err)
	}
	var snap *covsnap.CounterSnapshot
	snap, err = DecodeCounters(inst.buf.Bytes())
	if err != nil {
		return nil, eh.Errorf("unable to decode this binary's coverage counters: %w", err)
	}
	return inst.acc.Fold(snap, time.Now().UnixMilli())
}

// Meta returns the build's decoded lookup profile.
func (inst *Sampler) Meta() (meta *covsnap.MetaProfile) {
	return inst.acc.Meta()
}

// MetaBlob returns the raw meta-data blob of this build — the once-per-hash
// payload the persistence tee ingests (ADR-0169 §SD6). Callers must not
// mutate it.
func (inst *Sampler) MetaBlob() (blob []byte) {
	return inst.metaBlob
}

// Status returns the current absolute cumulative totals.
func (inst *Sampler) Status() (status covsnap.RunStatus) {
	return inst.acc.Status()
}

// Seq returns the number of samples folded so far.
func (inst *Sampler) Seq() (seq uint64) {
	return inst.acc.Seq()
}

// CoveredBitmap returns a clone of the cumulative covered set.
func (inst *Sampler) CoveredBitmap() (covered *roaring.Bitmap) {
	return inst.acc.CoveredBitmap()
}

// Close exists for producer-side symmetry with other samplers; the
// coverage sampler holds no resources.
func (inst *Sampler) Close() (err error) {
	return nil
}
