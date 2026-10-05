package ir

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

func fpFactory(name string, methods ...string) *BuilderFactoryNode {
	n := &BuilderFactoryNode{Name: naming.StylableName(name)}
	for _, m := range methods {
		n.BuilderMethods = append(n.BuilderMethods, Method{Spec: MethodSpec{Name: naming.StylableName(m)}})
	}
	return n
}

// The fingerprint is a function of what crosses the wire: the same IR gives
// the same value, and a change in opcode order, in a factory's methods or in
// a fetcher's return shape gives another.
func TestFingerprintFollowsTheWire(t *testing.T) {
	base := func() []NodeI {
		return []NodeI{fpFactory("button", "small", "frame"), fpFactory("label"), &FetcherNode{Name: "fetchKeys"}}
	}
	fp := Fingerprint(base())
	require.Equal(t, fp, Fingerprint(base()), "deterministic")

	swapped := base()
	swapped[0], swapped[1] = swapped[1], swapped[0]
	require.NotEqual(t, fp, Fingerprint(swapped), "opcode order")

	methods := base()
	methods[0] = fpFactory("button", "frame", "small")
	require.NotEqual(t, fp, Fingerprint(methods), "method order")

	shape := base()
	shape[2] = &FetcherNode{Name: "fetchKeys", ReturnTypes: PlainArgumentSpec{
		Names: []naming.StylableName{"edge"},
		Types: []canonicaltypes.PrimitiveAstNodeI{ctabb.U8},
	}}
	require.NotEqual(t, fp, Fingerprint(shape), "a fetcher's reply shape")
}
