package llm

import (
	"context"
	"iter"
	"strconv"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// ScanCalls reads the durable rows since a point in time, oldest first,
// up to limit, as records; nil, nil without a store. The tests' view of
// what landed: keelson('llm_calls') reads the ring, and a reader of the
// table reads the trail views.
func (inst *Service) ScanCalls(ctx context.Context, since time.Time, limit int) (recs []CallRecord, err error) {
	opts := recordstore.ScanOpts{
		ExtraPredicate: trail.TrailColOrder + " >= fromUnixTimestamp64Nano(" + strconv.FormatInt(since.UTC().UnixNano(), 10) + ")",
		Limit:          limit,
	}
	ents, err := inst.cfg.Trail.Scan(func(st *trail.TrailStore) iter.Seq2[*trail.TrailEntity, error] { return st.ScanLlmCall(ctx, opts) })
	if err != nil {
		return nil, err
	}
	for _, ent := range ents {
		recs = append(recs, RecordOf(ent))
	}
	return
}
