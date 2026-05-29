//go:build llm_generated_opus47

package logbridge

import (
	"fmt"
)

// stringify renders a CBOR-decoded value that did not match any of the
// typed LogField slots. Used by both the envelope (asString) and the
// fields fan-out (makeField) so the choice of fall-back representation
// stays consistent. fmt.Sprintf %v is intentional: leeway readers want a
// human-readable rendering, not a wire-format round-trip — the original
// CBOR can still be recovered upstream by replaying zerolog's output if
// truly needed.
func stringify(v any) (s string) {
	s = fmt.Sprintf("%v", v)
	return
}
