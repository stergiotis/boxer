package evidence

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// row is a synthetic chained row: a text per vaulted component, a mark,
// and the chain fields. Its canonical item is its fields spelled out,
// chain fields left out, commitments in.
type row struct {
	ordinal, prev uint64
	digest        string
	mark          Mark
	name          string
	// content per component; a component present in commitments but
	// absent here is erased.
	content map[string]string
	commits []Commitment
	head    *struct {
		ordinal uint64
		digest  string
	}
}

func (inst *row) Ordinal() uint64 { return inst.ordinal }
func (inst *row) Prev() uint64    { return inst.prev }
func (inst *row) Digest() string  { return inst.digest }
func (inst *row) Mark() Mark      { return inst.mark }
func (inst *row) CanonicalItem() ([]byte, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "ordinal=%d;name=%s;mark=%s/%d/%s;", inst.ordinal, inst.name, inst.mark.Mark, inst.mark.Target, inst.mark.Component)
	for _, c := range inst.commits {
		fmt.Fprintf(&b, "commit[%s]=%s;", c.Component, c.Commitment)
	}
	if inst.head != nil {
		fmt.Fprintf(&b, "head=%d/%s;", inst.head.ordinal, inst.head.digest)
	}
	return []byte(b.String()), nil
}
func (inst *row) Commitments() []Commitment { return inst.commits }
func (inst *row) VaultItem(component string) ([]byte, error) {
	return []byte("vault[" + component + "]=" + inst.content[component]), nil
}
func (inst *row) Head() (uint64, string, bool) {
	if inst.head == nil {
		return 0, "", false
	}
	return inst.head.ordinal, inst.head.digest, true
}

var testScheme = NewScheme("evidence-test/v1")

// chain builds a chain from seed over the rows' content: each row's
// commitments are drawn, its digest chained.
func chain(t *testing.T, seed string, rows ...*row) (out []RowI) {
	t.Helper()
	prevOrd, prevDigest := uint64(0), seed
	for i, r := range rows {
		r.ordinal, r.prev = uint64(i+1), prevOrd
		r.commits = nil
		for comp, text := range r.content {
			opening := make([]byte, OpeningSize)
			copy(opening, []byte(comp+text))
			item := []byte("vault[" + comp + "]=" + text)
			r.commits = append(r.commits, Commitment{Component: comp, Commitment: testScheme.Commit(opening, item), Opening: opening, Present: true})
		}
		sortCommits(r.commits)
		item, err := r.CanonicalItem()
		require.NoError(t, err)
		r.digest = testScheme.ChainDigest(prevDigest, item)
		prevOrd, prevDigest = r.ordinal, r.digest
		out = append(out, r)
	}
	return
}

func sortCommits(cs []Commitment) {
	for i := 1; i < len(cs); i++ {
		for j := i; j > 0 && cs[j].Component < cs[j-1].Component; j-- {
			cs[j], cs[j-1] = cs[j-1], cs[j]
		}
	}
}

// erase blanks one component of a row: content and opening gone, the
// commitment standing.
func erase(r *row, component string) {
	delete(r.content, component)
	for i := range r.commits {
		if r.commits[i].Component == component {
			r.commits[i].Opening, r.commits[i].Present = nil, false
		}
	}
}

func TestTheChainHolds(t *testing.T) {
	rows := chain(t, "seed",
		&row{name: "a", content: map[string]string{"body": "hello"}},
		&row{name: "b"},
		&row{name: "c", content: map[string]string{"body": "x", "args": "{}"}},
	)
	broken, why, err := Verify(testScheme, "seed", rows)
	require.NoError(t, err)
	require.Equal(t, BreakNone, why)
	require.Zero(t, broken)
}

func TestAChangedItemBreaksTheChainAtItsRow(t *testing.T) {
	rows := chain(t, "seed", &row{name: "a"}, &row{name: "b"}, &row{name: "c"})
	rows[1].(*row).name = "B"
	broken, why, err := Verify(testScheme, "seed", rows)
	require.NoError(t, err)
	require.Equal(t, uint64(2), broken)
	require.Equal(t, BreakDigest, why)
}

func TestARowThatDoesNotNameItsPredecessorBreaks(t *testing.T) {
	rows := chain(t, "seed", &row{name: "a"}, &row{name: "b"})
	rows[1].(*row).prev = 0
	broken, why, _ := Verify(testScheme, "seed", rows)
	require.Equal(t, uint64(2), broken)
	require.Equal(t, BreakPrev, why)
}

func TestAChangedBodyBreaksAtItsComponent(t *testing.T) {
	rows := chain(t, "seed", &row{name: "a", content: map[string]string{"body": "hello", "args": "{}"}})
	rows[0].(*row).content["args"] = "{tampered}"
	broken, why, _ := Verify(testScheme, "seed", rows)
	require.Equal(t, uint64(1), broken)
	require.Equal(t, BreakCommitment, why)
}

func TestABlankedComponentInALiveScopeBreaks(t *testing.T) {
	rows := chain(t, "seed", &row{name: "a", content: map[string]string{"body": "hello"}}, &row{name: "b"})
	erase(rows[0].(*row), "body")
	broken, why, _ := Verify(testScheme, "seed", rows)
	require.Equal(t, uint64(1), broken)
	require.Equal(t, BreakUnexplained, why)
}

func TestAnErasedMarkExplainsItsComponentAndNoOther(t *testing.T) {
	first := &row{name: "a", content: map[string]string{"body": "hello", "args": "{}"}}
	rows := chain(t, "seed", first, &row{name: "erase", mark: Mark{Mark: MarkErased, Target: 1, Component: "args"}})
	erase(first, "args")
	broken, why, _ := Verify(testScheme, "seed", rows)
	require.Zero(t, broken, why.String())
	// The other component is not explained by it.
	erase(first, "body")
	broken, why, _ = Verify(testScheme, "seed", rows)
	require.Equal(t, uint64(1), broken)
	require.Equal(t, BreakUnexplained, why)
}

func TestAnErasedMarkNamingNoComponentExplainsAll(t *testing.T) {
	first := &row{name: "a", content: map[string]string{"body": "hello", "args": "{}"}}
	rows := chain(t, "seed", first, &row{name: "erase", mark: Mark{Mark: MarkErased, Target: 1}})
	erase(first, "args")
	erase(first, "body")
	broken, why, _ := Verify(testScheme, "seed", rows)
	require.Zero(t, broken, why.String())
}

func TestDeletionKeepsTheChainAndNothingFollows(t *testing.T) {
	a := &row{name: "a", content: map[string]string{"body": "hello"}}
	b := &row{name: "b", content: map[string]string{"body": "world"}}
	rows := chain(t, "seed", a, b, &row{name: "deleted", mark: Mark{Mark: MarkDeleted}})
	erase(a, "body")
	erase(b, "body")
	broken, why, _ := Verify(testScheme, "seed", rows)
	require.Zero(t, broken, why.String())
	// A row appended after the deleted mark breaks, even when it chains.
	rows2 := chain(t, "seed", &row{name: "a"}, &row{name: "deleted", mark: Mark{Mark: MarkDeleted}}, &row{name: "late"})
	broken, why, _ = Verify(testScheme, "seed", rows2)
	require.Equal(t, uint64(3), broken)
	require.Equal(t, BreakAfterDeleted, why)
}

func TestAPresentComponentWithoutACommitmentBreaks(t *testing.T) {
	rows := chain(t, "seed", &row{name: "a", content: map[string]string{"body": "hello"}})
	// Chained with content and no commitment: the digest holds over the
	// item as written, and the component is what breaks.
	r := rows[0].(*row)
	r.commits[0].Commitment = ""
	item, err := r.CanonicalItem()
	require.NoError(t, err)
	r.digest = testScheme.ChainDigest("seed", item)
	broken, why, _ := Verify(testScheme, "seed", rows)
	require.Equal(t, uint64(1), broken)
	require.Equal(t, BreakCommitment, why)
}

func TestTwoSchemesDoNotOpenEachOthersCommitments(t *testing.T) {
	other := NewScheme("evidence-test/v2")
	opening := NewOpening()
	item := []byte("item")
	c := testScheme.Commit(opening, item)
	require.True(t, testScheme.Opens(opening, item, c))
	require.False(t, other.Opens(opening, item, c))
	require.NotEqual(t, testScheme.Name(), other.Name())
	require.True(t, strings.HasPrefix(testScheme.Name(), "keyed-blake3-256/v1/"))
}

func TestOpeningsRoundTripAsText(t *testing.T) {
	o := NewOpening()
	require.Len(t, o, OpeningSize)
	require.Equal(t, o, OpeningOf(OpeningText(o)))
	require.Nil(t, OpeningOf(""))
	require.Nil(t, OpeningOf("zz"))
	require.Nil(t, OpeningOf(OpeningText(o)[2:]))
}

func TestMarksParse(t *testing.T) {
	for _, m := range []MarkE{MarkNone, MarkErased, MarkDeleted, MarkKeep} {
		p, ok := ParseMark(m.String())
		require.True(t, ok)
		require.Equal(t, m, p)
		require.True(t, m.IsValid())
	}
	_, ok := ParseMark("other")
	require.False(t, ok)
	require.False(t, MarkE(9).IsValid())
}
