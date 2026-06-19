package marshallreflect_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
)

// ipDrone routes [N]byte fields through [N]byte sections but labels them IPv4 /
// IPv6 via `,ct=`. The Go/wire shape is unchanged; only the Plan's canonical
// gains the network type, so Plan-consuming tooling sees IPv4/IPv6.
type ipDrone struct {
	_   struct{} `kind:"ipd"`
	Id  uint64   `lw:",id"`
	Src [4]byte  `lw:"src,ipv4Section,ct=v"`
	Dst [16]byte `lw:"dst,ipv6Section,ct=w"`
}

// ctMismatchDrone applies ct=w (IPv6, [16]byte) to a [4]byte field — a reshape,
// which the override forbids.
type ctMismatchDrone struct {
	_  struct{} `kind:"ctm"`
	Id uint64   `lw:",id"`
	X  [4]byte  `lw:"x,sec,ct=w"`
}

// ctPlainDrone puts ct= on a plain column, which carries no attribute canonical.
type ctPlainDrone struct {
	_  struct{} `kind:"ctp"`
	Id uint64   `lw:",id,ct=u64"`
}

// ctBadDrone gives ct= an unparseable canonical string.
type ctBadDrone struct {
	_  struct{} `kind:"ctb"`
	Id uint64   `lw:",id"`
	Y  [4]byte  `lw:"y,sec,ct=@@@"`
}

// TestCanonicalOverride_NetworkFidelity confirms `,ct=` lifts the Plan canonical
// to the network type while leaving the Go/wire shape ([N]byte) untouched.
func TestCanonicalOverride_NetworkFidelity(t *testing.T) {
	plan, err := marshallreflect.PlanFor[ipDrone]()
	require.NoError(t, err)

	seen := map[string]bool{}
	for _, f := range plan.Fields {
		switch f.GoFieldName {
		case "Src":
			require.True(t, f.Canonical.IsNetworkNode(), "Src canonical should be a network node, got %T", f.Canonical)
			require.Equal(t, "[4]byte", f.GoType(), "Src Go shape unchanged")
			seen["Src"] = true
		case "Dst":
			require.True(t, f.Canonical.IsNetworkNode(), "Dst canonical should be a network node, got %T", f.Canonical)
			require.Equal(t, "[16]byte", f.GoType(), "Dst Go shape unchanged")
			seen["Dst"] = true
		}
	}
	require.True(t, seen["Src"] && seen["Dst"], "both network fields present")
}

// TestCanonicalOverride_Rejections covers the three guards: a reshape, ct= on a
// plain column, and an unparseable canonical.
func TestCanonicalOverride_Rejections(t *testing.T) {
	_, err := marshallreflect.PlanFor[ctMismatchDrone]()
	require.Error(t, err, "ct= reshape must be rejected")

	_, err = marshallreflect.PlanFor[ctPlainDrone]()
	require.Error(t, err, "ct= on a plain column must be rejected")

	_, err = marshallreflect.PlanFor[ctBadDrone]()
	require.Error(t, err, "unparseable ct= canonical must be rejected")
}
