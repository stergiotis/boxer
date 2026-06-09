package marshallreflect_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
)

// netShapes mirrors the Go types the leeway codegen emits for the network
// canonical types: address forms (v/w) are [4]byte / [16]byte, and the packed
// CIDR forms (vc/wc) append a per-value prefix byte -> [5]byte / [17]byte. All
// four are plain fixed-byte columns, so the codec carries them through the same
// size-agnostic [N]byte path as any other fixed blob (FixedByteArrayLen does
// not special-case particular sizes), with no marshalling-layer changes.
type netShapes struct {
	_   struct{} `kind:"netShapes"`
	Id  uint64   `lw:",id"`
	NK  []byte   `lw:",naturalKey"`
	V4  [4]byte  `lw:"v4,blob"`
	V4C [5]byte  `lw:"v4c,blob"`
	V6  [16]byte `lw:"v6,blob"`
	V6C [17]byte `lw:"v6c,blob"`
}

func TestPlanFor_NetworkPackedWidths(t *testing.T) {
	plan, err := marshallreflect.PlanFor[netShapes]()
	require.NoError(t, err)
	for name, want := range map[string]string{
		"V4":  "[4]byte",
		"V4C": "[5]byte",
		"V6":  "[16]byte",
		"V6C": "[17]byte",
	} {
		require.Equal(t, want, fieldByName(t, plan, name).GoType(), "field %s", name)
	}
}
