package mem_test

import (
	"context"
	"fmt"
	"time"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/procfs"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/mem"
)

// Example_basic shows the typical sample-once-and-print workflow against
// a fixture. In production code, omit the Proc / NowFunc options to
// read live /proc.
func Example_basic() {
	c, _ := mem.New(mem.Options{
		Proc:    procfs.New("testdata/modern"),
		NowFunc: func() time.Time { return time.Unix(1_700_000_000, 0) },
	})
	snap, _ := c.Sample(context.Background())
	fmt.Printf("total %d MiB, used %d MiB, available %d MiB\n",
		snap.TotalBytes>>20, snap.UsedBytes>>20, snap.AvailableBytes>>20)
	// Output: total 16000 MiB, used 4000 MiB, available 12000 MiB
}
