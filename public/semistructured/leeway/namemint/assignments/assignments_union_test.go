package assignments_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/assignments"
)

// The union check (ADR-0183 D1). A registry can only compare the vocabularies
// its own binary links, and no binary links them all; the committed tables can
// be read together by anything, which is what makes the disjointness claim
// total rather than per-link-set.
//
// It is not a formality: run against the tree before the width-32 re-key, this
// reported every jsonbench id as equal to a keelson runtime id, which is what
// two vocabularies sharing tag value 2 means.

func allGoldens(t *testing.T) map[string][]assignments.Assignment {
	t.Helper()
	found, err := assignments.FindGoldens(repoRoot(t))
	require.NoError(t, err)
	require.NotEmpty(t, found, "no assignment goldens found — the check would prove nothing")
	return found
}

// Ids carry their tag, so two vocabularies sharing a tag value collide here as
// colliding ids. That is the failure the tree already contains one rehearsal
// of: an app vocabulary picked the runtime's tag value, latent only because
// they share no table (ADR-0183 Context).
func TestCommittedAssignmentsAreGloballyDisjoint(t *testing.T) {
	owner := map[uint64]string{}
	for path, rows := range allGoldens(t) {
		for _, a := range rows {
			if prev, taken := owner[a.Id]; taken {
				t.Errorf("id %d is claimed twice: %s and %s/%s — one id, two meanings", a.Id, prev, path, a.Name)
				continue
			}
			owner[a.Id] = path + "/" + a.Name
		}
	}
	assert.NotEmpty(t, owner)
}

// Each vocabulary's ids all sit under one tag, and no two vocabularies share
// one. Disjointness follows from this structurally — the test above catches a
// collision, this one says why there is none.
func TestEachVocabularyOwnsOneTagValue(t *testing.T) {
	tagOwner := map[uint32]string{}
	for path, rows := range allGoldens(t) {
		require.NotEmpty(t, rows, "%s is empty", path)
		tv := assignments.TagValueOf(rows[0].Id).Value()
		for _, a := range rows {
			assert.EqualValues(t, tv, assignments.TagValueOf(a.Id).Value(),
				"%s: %s sits under a different tag value than its own vocabulary's", path, a.Name)
		}
		if prev, taken := tagOwner[tv]; taken {
			t.Errorf("tag value %d is used by two vocabularies: %s and %s", tv, prev, path)
			continue
		}
		tagOwner[tv] = path
	}
}
