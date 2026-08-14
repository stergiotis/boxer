package providers

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/vdd"
)

// membershipsRowsByName reads the provider's own output back into a map, so
// the assertions below go through the same path a query does.
func membershipsRowsByName(t *testing.T) map[string]int {
	t.Helper()
	rows := membershipRows()
	out := make(map[string]int, len(rows))
	for i, r := range rows {
		_, dup := out[r.name]
		require.Falsef(t, dup, "duplicate membership name %s", r.name)
		out[r.name] = i
	}
	return out
}

// TestMembershipsPublishesTheWireId is the property the whole table exists
// for: the id it publishes is the one a ref lane actually carries.
//
// A table that answered with any other number would be worse than no table —
// a predicate built from it would match nothing, silently, which is exactly
// the failure mode a reader reaches for the table to escape. Checked against
// GetId().Value(), which is what generated codecs embed.
func TestMembershipsPublishesTheWireId(t *testing.T) {
	rows := membershipRows()
	byName := membershipsRowsByName(t)
	require.NotEmpty(t, rows, "the in-tree registry declares memberships")

	for _, memb := range []struct {
		name string
		want uint64
	}{
		{"parent", uint64(vdd.MembParent.GetId().Value())},
		{"child", uint64(vdd.MembChild.GetId().Value())},
		{"natural-key", uint64(vdd.MembNaturalKey.GetId().Value())},
	} {
		i, ok := byName[memb.name]
		require.Truef(t, ok, "%s missing from the table", memb.name)
		require.Equalf(t, memb.want, rows[i].id, "%s publishes a different id than the wire carries", memb.name)
	}
}

// TestMembershipLookupAgreesWithTheTable pins the two halves of §SD4 to each
// other: what LW_GET resolves a name to client-side, and what the table
// publishes for a reader to join against, must be the same number. Two ways
// of answering one question is how they drift.
func TestMembershipLookupAgreesWithTheTable(t *testing.T) {
	rows := membershipRows()
	for _, r := range rows {
		if r.virtual {
			continue
		}
		got, err := MembershipLookup{}.LookupMembership(r.name)
		require.NoErrorf(t, err, "%s is in the table but not resolvable", r.name)
		require.Equalf(t, r.id, got, "%s resolves to a different id than the table publishes", r.name)
	}
}

// TestMembershipLookupRejectsUnknown pins that a miss is an error, not zero.
// Zero is a well-formed id, so returning it would expand into a predicate
// that matches nothing with nothing to say why.
func TestMembershipLookupRejectsUnknown(t *testing.T) {
	_, err := MembershipLookup{}.LookupMembership("noSuchMembershipAnywhere")
	require.Error(t, err)
}

// TestMembershipsTableIsStable pins the ordering. An introspection table
// that reordered itself between queries cannot be diffed, and the registry's
// own iteration order is an implementation detail.
func TestMembershipsTableIsStable(t *testing.T) {
	first := membershipRows()
	second := membershipRows()
	require.Equal(t, first, second)
	for i := 1; i < len(first); i++ {
		require.Lessf(t, first[i-1].name, first[i].name, "row %d is out of order", i)
	}
}

// TestMembershipsVirtualIsMarked pins the column that keeps a grouping node
// from reading as missing data: the `lw` family hangs off a virtual `leeway`
// entry, which is in the vocabulary but never on the wire.
func TestMembershipsVirtualIsMarked(t *testing.T) {
	rows := membershipRows()
	byName := membershipsRowsByName(t)

	i, ok := byName["leeway"]
	require.True(t, ok, "the virtual grouping entry is in the vocabulary")
	require.True(t, rows[i].virtual, "a grouping node must be marked virtual")

	j, ok := byName["table-name"]
	require.True(t, ok)
	require.False(t, rows[j].virtual, "a real membership must not be marked virtual")
	require.Contains(t, rows[j].parents, "leeway", "its virtual parent is what groups it")
}

// TestMembershipNamesAreFolded pins the spelling a reader has to type, and
// the asymmetry that goes with it.
//
// The registry folds every name to LowerSpinalCase on registration, so the
// Go declaration `MustBegin("naturalKey")` is stored — and published — as
// `natural-key`. A reader joining on the table must use the folded form,
// because that is the only spelling the registry kept. The LOOKUP is
// forgiving in a way the table cannot be: it retries through the registry's
// naming style, so LW_GET accepts either spelling.
func TestMembershipNamesAreFolded(t *testing.T) {
	byName := membershipsRowsByName(t)
	_, ok := byName["natural-key"]
	require.True(t, ok, "the table publishes the folded spelling")
	_, ok = byName["naturalKey"]
	require.False(t, ok, "the source spelling is not what the registry kept")

	folded, err := MembershipLookup{}.LookupMembership("natural-key")
	require.NoError(t, err)
	asWritten, err := MembershipLookup{}.LookupMembership("naturalKey")
	require.NoError(t, err, "the lookup folds, so a caller may spell it either way")
	require.Equal(t, folded, asWritten)
}

// TestMembershipsRegistered pins the table into the introspection registry,
// so keelson('memberships') resolves rather than reporting an unknown table.
func TestMembershipsRegistered(t *testing.T) {
	r := introspect.NewRegistry()
	require.NoError(t, RegisterStatic(r))

	p, ok := r.Lookup("memberships")
	require.True(t, ok, "keelson('memberships') must resolve")
	require.Equal(t, introspect.FreshnessStatic, p.Freshness())

	names := p.Schema().Fields()
	got := make([]string, 0, len(names))
	for _, f := range names {
		got = append(got, f.Name)
	}
	require.Subset(t, got, []string{"name", "id", "virtual", "parents"},
		"the columns a reader joins on must be present")
}
