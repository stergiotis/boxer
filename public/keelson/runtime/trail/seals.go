package trail

import (
	"context"
	"time"

	"github.com/zeebo/xxh3"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
)

// The recorder seals its flushes (ADR-0327 §SD5, law L34): its rows are
// written by many goroutines, so they do not chain; each batch the server
// takes carries one seal row naming the batch's other rows by the digest
// of their canonical items, chained to the run's previous durable seal.
// The seal stream is the run. A batch the server did not take has no
// durable seal; a batch the cap dropped yields an audit-gap row the next
// seal names.

// TrailSealScheme is the scheme the recorder's seals and row digests are
// under.
var TrailSealScheme = evidence.NewScheme("trail-seal/v1")

// digestLocked notes the row b committed, with its row digest, for the
// current batch's seal. A row whose item cannot be computed is logged and
// left for VerifySeals to report as unnamed. The caller holds mu.
func (inst *Recorder) digestLocked(b *TrailEntityBuilder, key string) {
	ent, mirrored := b.Entity()
	if !mirrored {
		return
	}
	item, err := inst.canon.CanonicalItem(&ent)
	if err != nil {
		inst.log.Error().Err(err).Str("key", key).Msg("trail: a row's canonical item could not be computed; no seal names it")
		return
	}
	inst.curRows = append(inst.curRows, evidence.SealedRow{Id: ent.ID, Digest: TrailSealScheme.RowDigest(item)})
}

// seal puts b's seal into its store, chained to the last durable seal,
// once: a batch retried after a failed flush keeps the seal it has, since
// nothing lands ahead of it, and a batch the cap drops takes its seal
// with it. The caller holds flushMu.
func (inst *Recorder) seal(b *heldBatch) (err error) {
	if b.sealId != 0 {
		return nil
	}
	at := time.Now().UTC()
	key := "seal|" + inst.unique(at)
	id := xxh3.HashString(key)
	row := SealOf(TrailSealScheme, inst.run, inst.lastSeal.Id, inst.lastSeal.Digest, b.sealed)
	row.Id = id
	bld := b.store.Begin(id, at, TrailEnvelope{NaturalKey: []byte(key)})
	bld.AddOrigin(Origin{Id: id, Run: inst.run})
	bld.AddEvidenceSeal(row)
	if err = bld.Commit(); err != nil {
		return eh.Errorf("trail: seal: %w", err)
	}
	b.sealId, b.sealDigest = id, row.Digest
	return
}

// VerifySeals is law L34 over the run named run, as its rows read back:
// the run's seals chain from the stream's start, every row a seal names is
// there with the digest the seal names, and no row is named twice. broken
// is the first break, zero when the stream holds; unnamed the run's rows
// no seal names, other than seals — rows that landed without their seal,
// or that something other than a recorder wrote. Nothing without a store;
// buffered rows are not seen until a flush.
func (inst *Recorder) VerifySeals(ctx context.Context, run string) (broken evidence.SealBreak, unnamed []uint64, err error) {
	if inst == nil {
		return
	}
	inst.readMu.Lock()
	defer inst.readMu.Unlock()
	if inst.read == nil {
		return
	}
	var seals []evidence.Seal
	present := make(map[uint64]string)
	pred := "has(" + valueColumn("symbol") + ", " + marshalling.EscapeString(run) + ")"
	for ent, serr := range inst.read.ScanOrigin(ctx, recordstore.ScanOpts{ExtraPredicate: pred}) {
		if serr != nil {
			return broken, nil, eh.Errorf("trail: read the run's rows: %w", serr)
		}
		if ent == nil || !ent.Origin.Has || ent.Origin.Val.Run != run {
			continue
		}
		if ent.EvidenceSeal.Has {
			seals = append(seals, ent.EvidenceSeal.Val.Seal(ent.ID))
			continue
		}
		item, ierr := inst.canon.CanonicalItem(ent)
		if ierr != nil {
			return broken, nil, eh.Errorf("trail: canonical item of row %d: %w", ent.ID, ierr)
		}
		present[ent.ID] = TrailSealScheme.RowDigest(item)
	}
	broken, unnamed = evidence.VerifySeals(TrailSealScheme, "", seals, present)
	return
}
