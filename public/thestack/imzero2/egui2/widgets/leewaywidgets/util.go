package leewaywidgets

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// emDash is the placeholder rendered for absent / empty value cells.
// "—" reads as "no data" rather than as an actual data character — the
// table emitter wraps it in italic-weak styling so it visually recedes.
const emDash = "—"

// stylableNamesToStrings flattens a slice of naming.StylableName values
// into a parallel slice of their plain-string representations, in order.
// Used by Table2CardEmitter to capture column names at section start so
// they can be paired with cell values during the per-row stream.
func stylableNamesToStrings(in []naming.StylableName) (out []string) {
	out = make([]string, len(in))
	for i, n := range in {
		out[i] = n.String()
	}
	return
}
