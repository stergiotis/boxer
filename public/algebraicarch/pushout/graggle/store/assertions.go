//go:build llm_generated_opus47

package store

import t "github.com/stergiotis/pebble2impl/src/go/public/algebraicarch/pushout/graggle/types"

// Compile-time interface assertions.
var (
	_ t.GraphReaderI  = (*Graggle)(nil)
	_ t.GraphWriterI  = (*Graggle)(nil)
	_ t.GraphStoreI   = (*Graggle)(nil)
	_ t.InspectableI  = (*Graggle)(nil)
	_ t.VisualizableI = (*Graggle)(nil)
)