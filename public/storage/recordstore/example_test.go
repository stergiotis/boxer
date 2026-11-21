package recordstore_test

import (
	"context"
	"fmt"
	"iter"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/internal/example"
)

// printingExecutor stands in for ClickHouse: it prints what a store would
// insert and answers no query.
type printingExecutor struct{}

func (printingExecutor) Exec(ctx context.Context, sql string) error { return nil }

func (printingExecutor) QueryArrow(ctx context.Context, sql string) iter.Seq2[arrow.RecordBatch, error] {
	return func(yield func(arrow.RecordBatch, error) bool) {}
}

func (printingExecutor) InsertArrow(ctx context.Context, table string, records []arrow.RecordBatch) error {
	rows := int64(0)
	for _, r := range records {
		rows += r.NumRows()
	}
	fmt.Println("insert into", table, rows, "rows")
	return nil
}

var _ recordstore.ExecutorI = printingExecutor{}

// ExampleExecutorI drives a generated record store (the device store of the
// example package) through an executor of its own. Ingest buffers; nothing
// reaches the executor until Flush, which inserts the batch as Arrow
// records. In production the executor is chexec's, over a ClickHouse server
// or clickhouse local.
func ExampleExecutorI() {
	ctx := context.Background()
	store := example.NewDeviceStore(printingExecutor{}, nil, example.DeviceStoreConfig{})
	err := store.IngestBattery(recordstore.SeqTs(1), []example.Battery{{ID: 1, Charge: 80}, {ID: 2, Charge: 55}})
	if err != nil {
		panic(err)
	}
	fmt.Println("buffered:", store.Buffered())
	n, err := store.Flush(ctx)
	if err != nil {
		panic(err)
	}
	fmt.Println("flushed:", n)
	// Output:
	// buffered: 2
	// insert into device 2 rows
	// flushed: 2
}
