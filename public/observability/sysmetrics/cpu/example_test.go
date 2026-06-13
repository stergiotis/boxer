package cpu_test

import (
	"context"
	"fmt"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/cpu"
)

// Example_loop shows a 1 Hz CPU sampling loop. The first call returns
// zero percentages (no prior tick); subsequent calls report deltas.
func Example_loop() {
	c, err := cpu.New(cpu.Options{})
	if err != nil {
		fmt.Println("init error:", err)
		return
	}
	// First sample primes the prior-tick state.
	_, _ = c.Sample(context.Background())
	// Real loops sleep 1s here and Sample again to read a meaningful
	// delta. We omit the sleep in this Example to keep godoc fast.
	_ = c
	fmt.Println("primed")
	// Output: primed
}
