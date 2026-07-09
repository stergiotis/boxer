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
	Src uint32   `lw:"src,ipv4Section,ct=v"`
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
// to the network type while leaving the Go/wire shape untouched — uint32 for an
// IPv4 host (its ClickHouse Arrow shape), [N]byte for IPv6 and the CIDR lanes.
func TestCanonicalOverride_NetworkFidelity(t *testing.T) {
	plan, err := marshallreflect.PlanFor[ipDrone]()
	require.NoError(t, err)

	seen := map[string]bool{}
	for _, f := range plan.Fields {
		switch f.GoFieldName {
		case "Src":
			require.True(t, f.Canonical.IsNetworkNode(), "Src canonical should be a network node, got %T", f.Canonical)
			require.Equal(t, "uint32", f.GoType(), "Src (IPv4 host) Go shape is uint32")
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

// ipCidrDrone carries CIDR-form addresses: IPv4 CIDR packs into [5]byte (4
// address + 1 prefix-length), IPv6 CIDR into [17]byte.
type ipCidrDrone struct {
	_    struct{} `kind:"ipc"`
	Id   uint64   `lw:",id"`
	Net4 [5]byte  `lw:"net4,cidr4Section,ct=vc"`
	Net6 [17]byte `lw:"net6,cidr6Section,ct=wc"`
}

// TestCanonicalOverride_CIDR confirms the variable-width CIDR canonicals reach
// the Plan via ct= and that the override's [N]byte width is validated.
func TestCanonicalOverride_CIDR(t *testing.T) {
	plan, err := marshallreflect.PlanFor[ipCidrDrone]()
	require.NoError(t, err)

	got := map[string]string{}
	for _, f := range plan.Fields {
		switch f.GoFieldName {
		case "Net4", "Net6":
			require.True(t, f.Canonical.IsNetworkNode(), "%s should carry a network canonical", f.GoFieldName)
			got[f.GoFieldName] = f.GoType()
		}
	}
	require.Equal(t, "[5]byte", got["Net4"], "IPv4 CIDR is address + prefix byte")
	require.Equal(t, "[17]byte", got["Net6"], "IPv6 CIDR is address + prefix byte")
}
