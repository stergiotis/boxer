package fixable

import (
	"errors"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// PatchHash stands in for a named type whose name is a better field key than
// any of its usual variable names.
type PatchHash [4]byte

func (inst PatchHash) String() (s string) { return "hash" }

// NodeID is a named string type: the key comes from the type, the value needs
// a conversion to reach Str.
type NodeID string

var ErrNotApplied = errors.New("not applied")

// The value sits at the tail of the clause and the argument names itself.
func tailValueNamedArgument(path string) (err error) {
	return eh.Errorf("open SBOM %q: %w", path, ErrNotApplied)
}

// The argument's own name is too short to be worth querying, so the key comes
// from its type instead.
func keyFromTypeName(h PatchHash) (err error) {
	return eh.Errorf("patch %s: %w", h, ErrNotApplied)
}

// Not name-shaped at all — an index expression has no name to borrow, but its
// named type does, and the named string type needs the conversion spelled.
func keyFromTypeOfIndexExpr(ids []NodeID) (err error) {
	return eh.Errorf("duplicate node id %q", ids[0])
}

// A named integer reaches a typed method through a conversion.
type StageE uint8

func namedIntegerConversion(stage StageE) (err error) {
	return eh.Errorf("unknown stage %d", stage)
}
