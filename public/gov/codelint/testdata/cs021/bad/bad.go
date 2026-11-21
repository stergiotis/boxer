package bad

import (
	"context"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/db/clickhouse/chclient"
)

func insert(ctx context.Context, cli *chclient.Client, records []arrow.RecordBatch) error {
	return cli.InsertArrow(ctx, "boxer.facts", records) // want a CS021 finding here
}

func insertSettings(ctx context.Context, cli *chclient.Client, records []arrow.RecordBatch) error {
	return cli.InsertArrowSettings(ctx, "boxer.facts", records, nil) // want a CS021 finding here
}

func insertStream(ctx context.Context, cli *chclient.Client) error {
	return cli.InsertStream(ctx, "INSERT INTO boxer.facts FORMAT Native", strings.NewReader(""), chclient.StreamOptions{}) // want a CS021 finding here
}

func methodValue(cli *chclient.Client) func(context.Context, string, []arrow.RecordBatch) error {
	return cli.InsertArrow // want a CS021 finding here
}
