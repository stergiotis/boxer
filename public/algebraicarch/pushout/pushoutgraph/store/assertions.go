package store

import t "github.com/stergiotis/boxer/public/algebraicarch/pushout/pushoutgraph/types"

// Compile-time interface assertions.
var (
	_ t.GraphReaderI  = (*PushoutGraph)(nil)
	_ t.GraphWriterI  = (*PushoutGraph)(nil)
	_ t.GraphStoreI   = (*PushoutGraph)(nil)
	_ t.InspectableI  = (*PushoutGraph)(nil)
	_ t.VisualizableI = (*PushoutGraph)(nil)
)
