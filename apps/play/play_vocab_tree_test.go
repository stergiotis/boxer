package play

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// vocabOutlineKindsAt returns the nodes of one kind, as (node, label) pairs.
func vocabOutlineKindsAt(o vocabOutline, kind vocabNodeKindE) (nodes []int32, labels []string) {
	for i, n := range o.Nodes {
		if n.Kind == kind {
			nodes = append(nodes, int32(i))
			labels = append(labels, o.Tree.Labels[i])
		}
	}
	return
}

// TestVocabOutlineIsAValidTree pins the widget's own precondition. tree.Render
// refuses a broken hierarchy and draws the error in place of the outline, so a
// builder that emits a dangling parent takes the whole panel out.
func TestVocabOutlineIsAValidTree(t *testing.T) {
	o := buildVocabOutline(testVocabDeclared(t), nil)
	require.NoError(t, o.Tree.Validate())
	assert.Equal(t, o.Tree.Len(), len(o.Nodes), "the metadata column shares the tree's node index")
	assert.Len(t, o.Tree.Keys, o.Tree.Len(), "a short key column is filed half by key and half by index")
}

// TestVocabOutlineKeysAreUnique is the one the widget cannot check for us:
// tree.State treats a shared key as a shared identity, and ADR-0176 documents
// duplicate keys as undetected rather than rejected. Two rows under one key
// expand and select together, which reads as a widget bug.
func TestVocabOutlineKeysAreUnique(t *testing.T) {
	declared := testVocabDeclared(t)
	installed := map[string]string{
		"myOwnHelper": "CREATE FUNCTION myOwnHelper AS (x) -> x",
		"CO_GATHER":   "CREATE FUNCTION CO_GATHER AS (lane, sel) -> x",
	}
	o := buildVocabOutline(append(declared, vocabExtras(installed, declared)...), nil)

	seen := make(map[string]int, o.Tree.Len())
	for i, k := range o.Tree.Keys {
		assert.NotEmptyf(t, k, "node %d has no key", i)
		if prev, dup := seen[k]; dup {
			t.Errorf("key %q is shared by nodes %d (%s) and %d (%s)",
				k, prev, o.Tree.Labels[prev], i, o.Tree.Labels[i])
		}
		seen[k] = i
	}
}

// TestVocabOutlineGroupsBySectionThenFamily pins the shape: three populations
// at depth 0, families under them, functions under families. It is the whole
// point of the file — a function reparented to a section would flatten the
// grouping back to what it replaced without failing anything else.
func TestVocabOutlineGroupsBySectionThenFamily(t *testing.T) {
	o := buildVocabOutline(testVocabDeclared(t), nil)

	sections, _ := vocabOutlineKindsAt(o, vocabNodeSection)
	require.Len(t, sections, 3, "one row per population, always")
	for _, s := range sections {
		assert.EqualValues(t, -1, o.Tree.Parents[s], "a population is a root")
	}

	families, _ := vocabOutlineKindsAt(o, vocabNodeFamily)
	require.NotEmpty(t, families)
	for _, f := range families {
		parent := o.Tree.Parents[f]
		require.GreaterOrEqual(t, parent, int32(0))
		assert.Equal(t, vocabNodeSection, o.Nodes[parent].Kind, "a family hangs off a population")
	}

	funcs, _ := vocabOutlineKindsAt(o, vocabNodeFunc)
	require.NotEmpty(t, funcs)
	for _, fn := range funcs {
		parent := o.Tree.Parents[fn]
		require.GreaterOrEqual(t, parent, int32(0))
		assert.Equal(t, vocabNodeFamily, o.Nodes[parent].Kind, "a function hangs off a family")
		assert.Equal(t, o.Nodes[parent].Where, o.Nodes[fn].Where,
			"a family sits entirely inside one population — ADR-0174 §SD1 still decides the sections")
	}
}

// TestVocabOutlineCounts pins that the count each grouping row shows is the
// number of functions actually beneath it. It is the number a reader uses to
// decide whether to open a family, so a stale one is worse than none.
func TestVocabOutlineCounts(t *testing.T) {
	o := buildVocabOutline(testVocabDeclared(t), nil)

	beneath := make(map[int32]int, o.Tree.Len())
	total := 0
	for i, n := range o.Nodes {
		if n.Kind != vocabNodeFunc {
			continue
		}
		total++
		fam := o.Tree.Parents[int32(i)]
		beneath[fam]++
		beneath[o.Tree.Parents[fam]]++
	}
	for node, want := range beneath {
		assert.Equalf(t, want, o.Nodes[node].Count, "%s counts wrong", o.Tree.Labels[node])
	}
	assert.Equal(t, total, len(testVocabDeclared(t)), "every entry reaches a row")
}

// TestVocabOutlineKeepsEmptySections pins what a filter may and may not remove.
// A population that vanishes when nothing in it matches reads as "this build
// has none of those", which is the confusion ADR-0174 exists to end; an empty
// FAMILY is dropped, because a screen of them would bury the matches.
func TestVocabOutlineKeepsEmptySections(t *testing.T) {
	none := buildVocabOutline(testVocabDeclared(t), func(vocabEntry) bool { return false })

	sections, _ := vocabOutlineKindsAt(none, vocabNodeSection)
	assert.Len(t, sections, 3, "the three populations survive a filter that matches nothing")
	for _, s := range sections {
		assert.Zerof(t, none.Nodes[s].Count, "%s should count nothing", none.Tree.Labels[s])
	}

	families, _ := vocabOutlineKindsAt(none, vocabNodeFamily)
	assert.Empty(t, families, "an emptied family is dropped")
	funcs, _ := vocabOutlineKindsAt(none, vocabNodeFunc)
	assert.Empty(t, funcs)

	// And a filter that admits one name leaves exactly that row, under its own
	// family, under its own population.
	one := buildVocabOutline(testVocabDeclared(t), func(e vocabEntry) bool { return e.Name == "LW_CO_GATHER" })
	_, labels := vocabOutlineKindsAt(one, vocabNodeFunc)
	require.Len(t, labels, 1)
	assert.Contains(t, labels[0], "LW_CO_GATHER")
	families, _ = vocabOutlineKindsAt(one, vocabNodeFamily)
	assert.Len(t, families, 1, "only the matching row's family is kept")
}

// TestVocabExtraFamiliesMatchTheModel pins the two family names the panel
// opens closed against the ones vocabExtras actually mints. They are matched
// by label, so a reworded family in the model would silently stop collapsing —
// and the extras are the population whose size this build does not bound.
func TestVocabExtraFamiliesMatchTheModel(t *testing.T) {
	declared := testVocabDeclared(t)
	installed := map[string]string{
		"myOwnHelper": "CREATE FUNCTION myOwnHelper AS (x) -> x",
		"CO_GATHER":   "CREATE FUNCTION CO_GATHER AS (lane, sel) -> x", // a withdrawn spelling
	}
	extras := vocabExtras(installed, declared)
	require.Len(t, extras, 2, "one undeclared helper and one withdrawn spelling")

	closed := vocabExtraFamilies()
	for _, e := range extras {
		assert.Containsf(t, closed, e.Family,
			"%s lands in family %q, which the outline does not know to close", e.Name, e.Family)
	}

	// And they do reach the outline as families of their own, rather than
	// merging into a declared one.
	o := buildVocabOutline(append(declared, extras...), nil)
	_, families := vocabOutlineKindsAt(o, vocabNodeFamily)
	for _, want := range closed {
		assert.Contains(t, families, want)
	}
}
