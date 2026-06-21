package container_test

import (
	"context"
	"fmt"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/container"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

// Example_detect shows the one-shot host-classifier API. The detector
// is stateless; subsequent calls re-probe.
func Example_detect() {
	d, err := container.New(container.Options{})
	if err != nil {
		fmt.Println("init error:", err)
		return
	}
	info, err := d.Detect(context.Background())
	if err != nil {
		fmt.Println("detect error:", err)
		return
	}
	// On a non-containerized host: Engine == EngineNone, Detail == "".
	switch info.Engine {
	case sysmsnap.EngineNone:
		fmt.Println("not in a container")
	default:
		fmt.Println("running in:", info.Engine.String())
	}
	// Output: not in a container
}
