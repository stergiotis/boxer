//go:build llm_generated_opus47

package store

import t "github.com/stergiotis/pebble2impl/src/go/public/algebraicarch/pushout/graggle/types"

// Compile-time interface assertions.
var (
	_ t.GraphReader  = (*Graggle)(nil)
	_ t.GraphWriter  = (*Graggle)(nil)
	_ t.GraphStore   = (*Graggle)(nil)
	_ t.Inspectable  = (*Graggle)(nil)
	_ t.Visualizable = (*Graggle)(nil)
)