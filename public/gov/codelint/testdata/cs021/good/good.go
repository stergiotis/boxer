package good

import (
	"context"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/db/clickhouse/chclient"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// Through an executor the registered transform applies.
func insert(ctx context.Context, cli *chclient.Client, records []arrow.RecordBatch) error {
	exec, err := storeexec.New(cli, nil)
	if err != nil {
		return err
	}
	return exec.InsertArrow(ctx, "boxer.facts", records)
}

// The interface's insert is the seam itself.
func through(ctx context.Context, exec recordstore.ExecutorI, records []arrow.RecordBatch) error {
	return exec.InsertArrow(ctx, "boxer.facts", records)
}

// Reads and statements through the client are not inserts.
func ping(ctx context.Context, cli *chclient.Client) error {
	return cli.Ping(ctx)
}

// A directive with a reason exempts one call.
func scratch(ctx context.Context, cli *chclient.Client, records []arrow.RecordBatch) error {
	return cli.InsertArrow(ctx, "bench.rows", records) //boxer:lint disable=CS021 reason="a benchmark's own table, never a transformed one"
}
