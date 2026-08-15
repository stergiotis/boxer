package marshallreflect_test

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/storegen"
	"github.com/stergiotis/boxer/public/keelson/vdd"
	anchor "github.com/stergiotis/boxer/public/semistructured/leeway/anchor"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
)

// The reflect path's ids, taken from the vocabulary registry that owns the
// names rather than written down beside the DTO (ADR-0183 D1).
//
// Both codec front-ends now resolve through one snapshot: the generated side
// takes it at generation time via storegen, and the reflect side takes the
// same map through NewRegistryLookup. A wrong id here is the sharpest of the
// silent failures — an unmatched membership is a legal "not present", so the
// row decodes with the field absent and nothing says why.
func TestNewRegistryLookupResolvesAgainstTheVocabulary(t *testing.T) {
	ids, err := storegen.MembershipIds(vdd.KeelsonHrNkRegistry)
	require.NoError(t, err)

	lookup, err := marshallreflect.NewRegistryLookup(ids)
	require.NoError(t, err)

	id, err := lookup.LookupMembership("naturalKey")
	require.NoError(t, err)
	assert.Equal(t, vdd.MembNaturalKey.GetId().Value(), id,
		"the reflect path resolves the id the registry minted")

	_, err = lookup.LookupMembership("thisNameIsNotRegistered")
	require.Error(t, err)
}

// The copy is the point: a caller that keeps editing its map cannot move the
// ids a lookup already handed out.
func TestNewRegistryLookupCopiesTheSnapshot(t *testing.T) {
	ids := map[string]uint64{"battery": 9}
	lookup, err := marshallreflect.NewRegistryLookup(ids)
	require.NoError(t, err)

	ids["battery"] = 4242
	got, err := lookup.LookupMembership("battery")
	require.NoError(t, err)
	assert.EqualValues(t, 9, got)
}

// An empty snapshot almost always means the vocabulary package was not linked.
// Without this the lookup would exist and fail every membership one at a time,
// which reads like a DTO problem rather than a build one.
func TestNewRegistryLookupRefusesAnEmptySnapshotAndZeroIds(t *testing.T) {
	_, err := marshallreflect.NewRegistryLookup(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "linked")

	_, err = marshallreflect.NewRegistryLookup(map[string]uint64{"battery": 0})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "zero id")
}

// End to end: a registry-backed lookup round-trips a DTO, so the constructor
// is usable where a hand-written literal was the only option before.
func TestRegistryLookupRoundTripsARow(t *testing.T) {
	ids, err := storegen.MembershipIds(vdd.KeelsonHrNkRegistry)
	require.NoError(t, err)
	lookup, err := marshallreflect.NewRegistryLookup(ids)
	require.NoError(t, err)

	type registryDrone struct {
		_ struct{} `kind:"registryDrone"`

		ID       uint64 `lw:",id"`
		Tracking []byte `lw:",naturalKey"`
		// `natural-key` is a keelson vocabulary membership; the DTO spells it
		// the way a `lw:` tag does and the snapshot bridges the two.
		Note string `lw:"naturalKey,symbol"`
	}

	table := anchor.NewInEntityTestTable(memory.NewGoAllocator(), 1)
	require.NoError(t, marshallreflect.Marshal(table, []registryDrone{
		{ID: 7, Tracking: []byte("TRK"), Note: "registry-resolved"},
	}, lookup))
	recs, err := table.TransferRecords(nil)
	require.NoError(t, err)
	defer func() {
		for _, r := range recs {
			r.Release()
		}
	}()

	readers, release := evReaders(t, recs[0], "symbol")
	defer release()
	var got []registryDrone
	require.NoError(t, marshallreflect.Unmarshal(readers, &got, lookup))
	require.Len(t, got, 1)
	assert.Equal(t, "registry-resolved", got[0].Note)
}
