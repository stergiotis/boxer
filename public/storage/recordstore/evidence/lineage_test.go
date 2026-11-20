package evidence

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// schemedRow is a row that records its scheme, as a generated chain row
// does.
type schemedRow struct {
	*row
	scheme string
}

func (inst *schemedRow) SchemeName() string { return inst.scheme }

// legacyItem is the canonical item of the test-only retired scheme: a form
// the current build no longer computes, so the row's own CanonicalItem is
// not what its digest is over.
func legacyItem(r RowI) ([]byte, error) {
	s := r.(*schemedRow)
	return fmt.Appendf(nil, "legacy;%d;%s", s.ordinal, s.name), nil
}

var (
	lineageOld = NewScheme("evidence-lineage-test/v1")
	lineageNew = NewScheme("evidence-lineage-test/v2")
)

// crossChain is one chain that moved schemes half way: rows 1–2 written
// under the retired scheme over its legacy item, with a vaulted component
// committed under it; rows 3–4 under the current scheme. Row 4 records a
// head over row 2.
func crossChain(t *testing.T) (rows []RowI) {
	t.Helper()
	prevOrd, prevDigest := uint64(0), "seed"
	for i := range 4 {
		r := &schemedRow{row: &row{ordinal: uint64(i + 1), prev: prevOrd, name: fmt.Sprintf("r%d", i+1)}}
		scheme := lineageNew
		if i < 2 {
			scheme = lineageOld
			opening := make([]byte, OpeningSize)
			copy(opening, r.name)
			r.content = map[string]string{"body": "text " + r.name}
			item := []byte("vault[body]=" + r.content["body"])
			r.commits = []Commitment{{Component: "body", Commitment: scheme.Commit(opening, item), Opening: opening, Present: true}}
		}
		if i == 3 {
			r.head = &struct {
				ordinal uint64
				digest  string
			}{ordinal: 2, digest: rows[1].Digest()}
		}
		r.scheme = scheme.Name()
		var item []byte
		var err error
		if i < 2 {
			item, err = legacyItem(r)
		} else {
			item, err = r.CanonicalItem()
		}
		require.NoError(t, err)
		r.digest = scheme.ChainDigest(prevDigest, item)
		prevOrd, prevDigest = r.ordinal, r.digest
		rows = append(rows, r)
	}
	return
}

// TestRetiredSchemeStillVerifies is the cross-version claim: after a store
// moves to a new scheme, a chain whose older rows record the retired scheme
// verifies as one chain, heads included — and stops verifying the moment
// the retired scheme leaves the lineage.
func TestRetiredSchemeStillVerifies(t *testing.T) {
	rows := crossChain(t)
	lineage := NewLineage(lineageNew, Retired{Scheme: lineageOld, Item: legacyItem})

	broken, why, err := VerifyLineage(lineage, "seed", rows)
	require.NoError(t, err)
	require.Equal(t, BreakNone, why)
	require.Zero(t, broken)

	anchored := []AnchoredHead{{Ordinal: 2, Digest: rows[1].Digest()}}
	require.Empty(t, VerifyHeadsLineage(lineage, "seed", rows, anchored))

	broken, why, _ = VerifyLineage(NewLineage(lineageNew), "seed", rows)
	require.Equal(t, uint64(1), broken)
	require.Equal(t, BreakScheme, why)

	// Tampering with a retired-scheme row still breaks at it.
	rows[0].(*schemedRow).name = "forged"
	broken, why, _ = VerifyLineage(lineage, "seed", rows)
	require.Equal(t, uint64(1), broken)
	require.Equal(t, BreakDigest, why)
}

// TestVerifyDispatchesThroughTheRegistry is Verify with only the current
// scheme in hand: the registered lineage supplies the retired one.
func TestVerifyDispatchesThroughTheRegistry(t *testing.T) {
	rows := crossChain(t)
	broken, why, _ := Verify(lineageNew, "seed", rows)
	require.Equal(t, BreakScheme, why, "nothing registered yet")
	require.Equal(t, uint64(1), broken)

	RegisterLineage(NewLineage(lineageNew, Retired{Scheme: lineageOld, Item: legacyItem}))
	broken, why, err := Verify(lineageNew, "seed", rows)
	require.NoError(t, err)
	require.Equal(t, BreakNone, why)
	require.Zero(t, broken)
	require.True(t, LineageOf(lineageNew).Knows(lineageOld.Name()))
	require.ElementsMatch(t, []string{lineageNew.Name(), lineageOld.Name()}, LineageOf(lineageNew).Names())
}

func TestLineageRefusesRetiringItsCurrentScheme(t *testing.T) {
	require.Panics(t, func() { NewLineage(lineageNew, Retired{Scheme: lineageNew}) })
}

// TestMemoisedRowsKeepTheirScheme: the wrapping a caller adds to read each
// item once must not hide the scheme a row records, nor the row a retired
// item builder reads.
func TestMemoisedRowsKeepTheirScheme(t *testing.T) {
	rows := Memoised(crossChain(t))
	lineage := NewLineage(lineageNew, Retired{Scheme: lineageOld, Item: legacyItem})
	broken, why, err := VerifyLineage(lineage, "seed", rows)
	require.NoError(t, err)
	require.Equal(t, BreakNone, why)
	require.Zero(t, broken)
}
