package trail

import (
	"context"

	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
)

// The recorder seals its flushes (ADR-0327 §SD5, law L34) through the
// shared writer (ADR-0330 §SD5): its rows are written by many goroutines,
// so they do not chain; each batch the server takes carries one seal row
// naming the batch's other rows by the digest of their canonical items,
// chained to the run's previous durable seal. The seal stream is the run.
// A batch the server did not take has no durable seal; a batch the cap
// dropped yields an audit-gap row the next seal names.

// VerifySeals is law L34 over the run named run, as its rows read back,
// and law L32 over its anchored seals: the run's seals chain from the
// stream's start, every row a seal names is there with the digest the
// seal names, no row is named twice, and each anchored ordinal holds the
// digest of the n-th seal. A row or a seal read twice — a batch stored
// twice after a lost acknowledgement — counts once. broken is the first
// break of the stream, zero when it holds; unnamed the run's rows no seal
// names, other than seals — rows that landed without their seal, or that
// something other than a recorder wrote; unanchored the anchored ordinals
// the stream does not reproduce, none without an anchor store. Nothing
// without a store; buffered rows are not seen until a flush.
func (inst *Recorder) VerifySeals(ctx context.Context, run string) (broken evidence.SealBreak, unnamed []uint64, unanchored []uint64, err error) {
	if inst == nil {
		return
	}
	return inst.rec.VerifySeals(ctx, run)
}
