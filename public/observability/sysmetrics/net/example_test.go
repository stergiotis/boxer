//go:build llm_generated_opus47

package net_test

import (
	"context"
	"fmt"

	pnet "github.com/stergiotis/boxer/public/observability/sysmetrics/net"
)

// Example_includeLoopback samples every network interface including
// the kernel loopback (filtered out by default).
func Example_includeLoopback() {
	c, err := pnet.New(pnet.Options{IncludeLoopback: true})
	if err != nil {
		fmt.Println("init error:", err)
		return
	}
	_, _ = c.Sample(context.Background())
	// In production: snap.Interfaces[i].{Rx,Tx}BytesPerSec is the
	// per-second rate computed against the prior Sample.
	fmt.Println("ok")
	// Output: ok
}
