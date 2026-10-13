package auditrec

import (
	"context"
	"iter"
	"regexp"
	"time"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit"
)

// BatchI is one batch of rows: a generated store opened for writing, as
// the recorder reaches it. The recorder uses a batch from one goroutine at
// a time.
type BatchI interface {
	// Event buffers one row holding o and e, begun under ctx (BeginCtx, so
	// a store generated with WrittenBy carries the identity on ctx), with
	// id, at and key; key goes where the store's envelope keeps a key, if
	// anywhere. item is the row's canonical item. On error the row is not
	// buffered.
	Event(ctx context.Context, id uint64, at time.Time, key string, o audit.Origin, e audit.AuditEvent) (item []byte, err error)
	// Seal buffers the batch's seal row with o, built by the store's
	// generated SealOf from s, and returns the seal's digest.
	Seal(id uint64, at time.Time, key string, o audit.Origin, s SealSpec) (digest string, err error)
	// Buffered is the rows buffered and not yet flushed.
	Buffered() int
	// Flush inserts what is buffered; on failure the rows stay for the
	// next Flush.
	Flush(ctx context.Context) error
	// Close discards what is unflushed and releases the batch.
	Close()
}

// StoreI is a store generated with CanonWire and Evidence{Seals: true}
// that binds audit.Origin and audit.AuditEvent, as the recorder reaches
// it. Its scans are called under one lock; NewBatch under another.
type StoreI[B BatchI] interface {
	// NewBatch opens a batch over the store's table, with o, when not nil,
	// as the generated store's write observer.
	NewBatch(o recordstore.WriteObserverI[uint64, time.Time]) B
	// Columns are the physical names the recorder's reads narrow on.
	Columns() Columns
	// ScanEvents reads the audit events under opts, each with the origin
	// its row carries (zero when it carries none).
	ScanEvents(ctx context.Context, opts recordstore.ScanOpts) iter.Seq2[audit.Event, error]
	// ScanRows reads the rows carrying an origin under opts: a seal as
	// the seal law reads it, any other row with its canonical item.
	ScanRows(ctx context.Context, opts recordstore.ScanOpts) iter.Seq2[Row, error]
	// Close releases the store's read side.
	Close()
}

// SealSpec is what a batch's seal is built from: the scheme, the stream
// (the recorder's run), the stream's last durable seal — zero and the
// seed for the first — and the rows the seal names. A binding hands it to
// the store's generated SealOf.
type SealSpec struct {
	Scheme     evidence.Scheme
	Stream     string
	Prev       uint64
	PrevDigest string
	Rows       []evidence.SealedRow
}

// Row is a stored row as the seal law reads it: its id and origin, and
// either the seal it is with the stream it names, or its canonical item.
type Row struct {
	Id     uint64
	Origin audit.Origin
	Seal   option.Option[StreamSeal]
	Item   []byte
}

// StreamSeal is a seal and the stream it names.
type StreamSeal struct {
	Stream string
	Seal   evidence.Seal
}

// Columns are the quoted physical column names the recorder's reads
// narrow on: the order column and the value columns of the symbol,
// stringArray and u64Array sections.
type Columns struct {
	Order       string
	Symbol      string
	StringArray string
	U64Array    string
}

var valueColumnPattern = regexp.MustCompile(`"tv:([A-Za-z0-9]+):value:[^"]*"`)

// ColumnsFromDDL is the Columns of a generated store: order is its
// <Store>ColOrder constant, ddl its generated CREATE TABLE statement,
// whose value columns are read out of it rather than spelled, so a change
// of encoding hints regenerates them.
func ColumnsFromDDL(order string, ddl string) (c Columns, err error) {
	cols := make(map[string]string, 8)
	for _, m := range valueColumnPattern.FindAllStringSubmatch(ddl, -1) {
		if _, ok := cols[m[1]]; !ok {
			cols[m[1]] = m[0]
		}
	}
	c = Columns{Order: order, Symbol: cols["symbol"], StringArray: cols["stringArray"], U64Array: cols["u64Array"]}
	if c.Order == "" || c.Symbol == "" || c.StringArray == "" || c.U64Array == "" {
		err = eh.Errorf("auditrec: the DDL lacks the order column or a value column of symbol, stringArray or u64Array")
	}
	return
}
