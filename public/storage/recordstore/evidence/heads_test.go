package evidence

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// anchored is the store's head for a chained row, received at t0+ordinal.
func anchored(rows []RowI, ordinal uint64, id string) AnchoredHead {
	return AnchoredHead{Ordinal: ordinal, Digest: rows[ordinal-1].Digest(),
		ReceiptAt: time.Unix(1_000, 0).Add(time.Duration(ordinal) * time.Second), ReceiptId: id}
}

// headRow is a row recording the head at covered, with the digest the
// chain has there.
func headRow(rows []RowI, covered uint64) *row {
	return &row{name: "head", mark: Mark{Mark: MarkKeep},
		head: &struct {
			ordinal uint64
			digest  string
		}{covered, rows[covered-1].Digest()}}
}

func TestLawL32Holds(t *testing.T) {
	rows := chain(t, "seed", &row{name: "a"}, &row{name: "b"})
	h := headRow(rows, 2)
	rows = chain(t, "seed", rows[0].(*row), rows[1].(*row), h)
	store := []AnchoredHead{anchored(rows, 2, "r1")}
	require.Empty(t, VerifyHeads(testScheme, "seed", rows, store))
}

func TestADroppedHeadRowBreaksTheAnchoredOrdinal(t *testing.T) {
	rows := chain(t, "seed", &row{name: "a"}, &row{name: "b"})
	store := []AnchoredHead{anchored(rows, 2, "r1")}
	// The store holds a head the journal does not record.
	require.Equal(t, []uint64{2}, VerifyHeads(testScheme, "seed", rows, store))
}

func TestAHeadRowTheStoreDoesNotHoldBreaks(t *testing.T) {
	rows := chain(t, "seed", &row{name: "a"}, &row{name: "b"})
	h := headRow(rows, 2)
	rows = chain(t, "seed", rows[0].(*row), rows[1].(*row), h)
	require.Equal(t, []uint64{3}, VerifyHeads(testScheme, "seed", rows, nil))
}

func TestAReanchoredTailBreaks(t *testing.T) {
	rows := chain(t, "seed", &row{name: "a"}, &row{name: "b"})
	h := headRow(rows, 2)
	rows = chain(t, "seed", rows[0].(*row), rows[1].(*row), h)
	first := anchored(rows, 2, "r1")
	// The tail is rewritten and the new head anchored later for the same
	// ordinal; the earliest receipt counts and the later one breaks it.
	rows[1].(*row).name = "B"
	rewritten := chain(t, "seed", rows[0].(*row), rows[1].(*row), headRow([]RowI{rows[0], rows[1]}, 2))
	rewritten = chain(t, "seed", rewritten[0].(*row), rewritten[1].(*row), headRow(rewritten, 2))
	later := anchored(rewritten, 2, "r2")
	later.ReceiptAt = first.ReceiptAt.Add(time.Minute)
	broken := VerifyHeads(testScheme, "seed", rewritten, []AnchoredHead{first, later})
	require.Contains(t, broken, uint64(2))
}

func TestABrokenChainBreaksEveryHeadPastIt(t *testing.T) {
	rows := chain(t, "seed", &row{name: "a"}, &row{name: "b"}, &row{name: "c"})
	h := headRow(rows, 3)
	rows = chain(t, "seed", rows[0].(*row), rows[1].(*row), rows[2].(*row), h)
	store := []AnchoredHead{anchored(rows, 3, "r1")}
	require.Empty(t, VerifyHeads(testScheme, "seed", rows, store))
	rows[1].(*row).name = "B"
	broken := VerifyHeads(testScheme, "seed", rows, store)
	require.Contains(t, broken, uint64(3))
	require.Contains(t, broken, uint64(4))
}

func TestDeletionKeepsTheHeads(t *testing.T) {
	a := &row{name: "a", content: map[string]string{"body": "hello"}}
	rows := chain(t, "seed", a, &row{name: "b"})
	h := headRow(rows, 2)
	rows = chain(t, "seed", a, rows[1].(*row), h, &row{name: "deleted", mark: Mark{Mark: MarkDeleted}})
	store := []AnchoredHead{anchored(rows, 2, "r1")}
	erase(a, "body")
	require.Empty(t, VerifyHeads(testScheme, "seed", rows, store))
}
