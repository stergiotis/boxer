package evidence

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// countingRow counts the item reads a law makes of it.
type countingRow struct {
	items, vaults int
	head          bool
}

func (r *countingRow) Ordinal() uint64 { return 1 }
func (r *countingRow) Prev() uint64    { return 0 }
func (r *countingRow) Digest() string  { return "" }
func (r *countingRow) CanonicalItem() ([]byte, error) {
	r.items++
	return []byte("item"), nil
}
func (r *countingRow) Mark() Mark                { return Mark{} }
func (r *countingRow) Commitments() []Commitment { return nil }
func (r *countingRow) VaultItem(component string) ([]byte, error) {
	r.vaults++
	if component == "missing" {
		return nil, errors.New("no row")
	}
	return []byte(component), nil
}

type countingHeadRow struct{ countingRow }

func (r *countingHeadRow) Head() (uint64, string, bool) { return 3, "d", true }

// A memoised row computes its canonical item and each vault item once,
// errors included, and answers Head as its row does.
func TestMemoisedRowsReadEachItemOnce(t *testing.T) {
	plain := &countingRow{}
	rows := Memoised([]RowI{plain, &countingHeadRow{}})
	for range 3 {
		item, err := rows[0].CanonicalItem()
		require.NoError(t, err)
		require.Equal(t, []byte("item"), item)
		v, err := rows[0].VaultItem("note")
		require.NoError(t, err)
		require.Equal(t, []byte("note"), v)
		_, err = rows[0].VaultItem("missing")
		require.Error(t, err)
	}
	require.Equal(t, 1, plain.items)
	require.Equal(t, 2, plain.vaults, "one read per component, the failed one too")

	_, _, ok := rows[0].(HeadRowI).Head()
	require.False(t, ok, "a row without a head has none")
	ordinal, digest, ok := rows[1].(HeadRowI).Head()
	require.True(t, ok)
	require.Equal(t, uint64(3), ordinal)
	require.Equal(t, "d", digest)
}
