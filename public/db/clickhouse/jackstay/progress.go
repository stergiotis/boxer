package jackstay

import (
	"context"
	"sync/atomic"
	"time"
)

// pollWrittenRows advances rows by the INSERT's written_rows, read from the
// target's system.processes every period until stop is closed, and returns
// the last value it saw. It is best effort: a failed poll is skipped, and a
// query no longer in system.processes has finished.
func pollWrittenRows(ctx context.Context, q QueryI, queryId string, period time.Duration, rows *atomic.Int64, stop <-chan struct{}) (seen int64) {
	if rows == nil {
		<-stop
		return
	}
	sql := "SELECT toUInt64(written_rows) AS n FROM system.processes WHERE query_id = " + QuoteString(queryId) + jsonSettings
	t := time.NewTicker(period)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ctx.Done():
			return
		case <-t.C:
		}
		got, err := queryRows[countRow](ctx, q, sql)
		if err != nil || len(got) != 1 {
			continue
		}
		n := int64(got[0].N)
		if n > seen {
			rows.Add(n - seen)
			seen = n
		}
	}
}
