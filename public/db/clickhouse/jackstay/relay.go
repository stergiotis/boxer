package jackstay

import (
	"context"
	"io"
	"sync/atomic"
	"time"

	gonanoid "github.com/matoous/go-nanoid/v2"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// countingReader counts the bytes read through it. n is atomic: the HTTP
// client may still be reading a request body when InsertStream returns.
type countingReader struct {
	r     io.Reader
	n     atomic.Int64
	total *atomic.Int64
}

func (inst *countingReader) Read(p []byte) (n int, err error) {
	n, err = inst.r.Read(p)
	inst.n.Add(int64(n))
	if inst.total != nil {
		inst.total.Add(int64(n))
	}
	return
}

// relay streams the Native rows of srcSpec from src into dstSpec's table on
// dst, undecoded and, when the source compressed them, still compressed. It
// returns the bytes that crossed and the rows it counted into opts.Rows, so a
// caller whose verification fails can wind them back.
func relay(ctx context.Context, src SourceI, dst ClientI, srcSpec *DigestSpec, dstSpec *DigestSpec, opts *SyncOptions, expectedRows uint64) (bytes uint64, counted int64, err error) {
	var queryId string
	queryId, err = gonanoid.New()
	if err != nil {
		err = eh.Errorf("unable to mint a query id: %w", err)
		return
	}
	queryId = "jackstay-" + queryId
	var body io.ReadCloser
	var encoding string
	body, encoding, err = src.stream(ctx, srcSpec, opts.Compression)
	if err != nil {
		err = eh.Errorf("unable to read source rows: %w", err)
		return
	}
	defer func() { _ = body.Close() }()
	stop := make(chan struct{})
	seenCh := make(chan int64, 1)
	go func() {
		seenCh <- pollWrittenRows(ctx, dst, queryId, max(opts.PollPeriod, 100*time.Millisecond), opts.Rows, stop)
	}()
	cr := &countingReader{r: body, total: opts.Bytes}
	err = dst.InsertStream(ctx, dstSpec.InsertNative(), cr, chclient.StreamOptions{QueryId: queryId, ContentEncoding: encoding})
	close(stop)
	counted = <-seenCh
	bytes = uint64(cr.n.Load())
	if err != nil {
		if opts.Rows != nil {
			opts.Rows.Add(-counted)
		}
		counted = 0
		err = eh.Errorf("unable to insert rows on the target: %w", err)
		return
	}
	if opts.Rows != nil {
		opts.Rows.Add(int64(expectedRows) - counted)
	}
	counted = int64(expectedRows)
	return
}
