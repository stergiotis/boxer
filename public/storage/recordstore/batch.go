package recordstore

import (
	"context"

	"github.com/apache/arrow-go/v18/arrow"

	gonanoid "github.com/matoous/go-nanoid/v2"

	"github.com/stergiotis/boxer/public/db/clickhouse/logcomment"
	"github.com/stergiotis/boxer/public/identity/callident"
)

// BatchIdT names one executor call — one insert attempt, one query, one
// statement (ADR-0295 §SD6). A generated store mints one per InsertArrow
// attempt and attaches it with [WithBatchId]; [ObserveExecutor] uses the id it
// finds and mints one otherwise; the executors stamp it into log_comment. The
// store's write observer, the executor event and the server's query log
// therefore name the same batch.
type BatchIdT string

// NewBatchId mints a batch id: a nanoid, ~126 random bits.
func NewBatchId() BatchIdT {
	return BatchIdT(gonanoid.Must())
}

type batchIdKey struct{}

// WithBatchId returns ctx carrying id, replacing any batch id already on it.
func WithBatchId(ctx context.Context, id BatchIdT) context.Context {
	return context.WithValue(ctx, batchIdKey{}, id)
}

// BatchIdFrom returns the batch id on ctx; ok is false when there is none.
func BatchIdFrom(ctx context.Context) (id BatchIdT, ok bool) {
	id, _ = ctx.Value(batchIdKey{}).(BatchIdT)
	ok = id != ""
	return
}

// LogComment is the log_comment stamp an executor attaches to a call made
// under ctx (ADR-0295 §SD3): the call identity and the batch id, in the shared
// logcomment format. It is "" when ctx carries neither, and an executor then
// sends no setting at all.
func LogComment(ctx context.Context) (s string) {
	ci, _ := callident.CallIdentityFrom(ctx)
	batch, _ := BatchIdFrom(ctx)
	s = logcomment.Marshal(logcomment.FromCallIdentity(ci, string(batch)))
	return
}

// PendingBatch is one insert a generated store's Flush attempted and the
// server did not acknowledge: the records, the batch id of the attempt —
// which a retry sends again as its deduplication token (ADR-0387) — the
// rows it carries, and how many of the write observer's kept rows are its.
// A store retries its pending batches in order, each alone.
type PendingBatch struct {
	Records []arrow.RecordBatch
	Batch   BatchIdT
	Rows    int
	Written int
}

// Release releases the batch's records.
func (inst PendingBatch) Release() {
	for _, rec := range inst.Records {
		rec.Release()
	}
}
