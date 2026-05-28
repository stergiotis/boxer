//go:build llm_generated_opus47

package disk_test

import (
	"context"
	"fmt"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/disk"
)

// Example_physicalOnly samples only physical filesystems (no tmpfs,
// no procfs), skipping swap entries.
func Example_physicalOnly() {
	c, err := disk.New(disk.Options{
		PhysicalOnly: true,
		SkipSwap:     true,
	})
	if err != nil {
		fmt.Println("init error:", err)
		return
	}
	_, _ = c.Sample(context.Background())
	// In production: iterate snap.Mounts, render Capacity per mount,
	// match snap.BlockDevices by Mount.BlockName for I/O rates.
	fmt.Println("ok")
	// Output: ok
}
