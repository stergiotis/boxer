//go:build llm_generated_opus47

package proc_test

import (
	"context"
	"fmt"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/proc"
)

// Example_iterator demonstrates the iter.Seq2[Info, error] surface.
// The eager-snapshot semantics mean break-out-early does not corrupt
// the prior-tick state.
func Example_iterator() {
	c, err := proc.New(proc.Options{})
	if err != nil {
		fmt.Println("init error:", err)
		return
	}
	for info, perr := range c.All(context.Background()) {
		if perr != nil {
			fmt.Println("iter error:", perr)
			break
		}
		_ = info
		// First sample reports CPUPercent=0 across the board (no prior
		// tick to delta against).
		break
	}
	fmt.Println("ok")
	// Output: ok
}
