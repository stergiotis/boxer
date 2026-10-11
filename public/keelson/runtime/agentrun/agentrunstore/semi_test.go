package agentrunstore

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/jsonshred"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

const args = `{"query":"select 1","limit":10,"flags":[true,null],"opts":{},"list":[],"ratio":0.5,"big":18446744073709551616}`

func vaultFill(t *testing.T, attrs []jsonshred.Attr) func(b *BodyRowEntityBuilder) error {
	return func(b *BodyRowEntityBuilder) error {
		b.AddBodyRow(BodyRow{Body: "hello"}).AddEndorsement(Endorsement{Digest: "d", Epoch: "t1", Classes: []string{"publish"}, Previous: "none"})
		return ContributeShredded(b, attrs)
	}
}

// A vault row with components and shredded arguments goes into the table
// and comes back: the components decode, the shredded attributes recompose
// to the canonical document, and the row read back hashes to the item the
// row as written hashed to.
func TestTheVaultRowRoundTrips(t *testing.T) {
	ctx := context.Background()
	exec, err := chexec.NewLocalExecutor(filepath.Join(t.TempDir(), "chdb"), memory.NewGoAllocator())
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}
	layout := Layout{Database: "semitest"}
	require.NoError(t, ProvisionIn(ctx, exec, layout))
	st := NewBodyStoreIn(exec, layout)
	defer st.Close()

	attrs, err := jsonshred.Explode([]byte(args), jsonshred.Options{})
	require.NoError(t, err)
	b := st.Begin(1, 3)
	require.NoError(t, vaultFill(t, attrs)(b))
	require.NoError(t, b.Commit())
	_, err = st.Flush(ctx)
	require.NoError(t, err)

	rows, err := st.ScanVault(ctx, BodyRowColKey+" = 1")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "hello", rows[0].Ent.BodyRow.Val.Body)
	assert.Equal(t, []string{"publish"}, rows[0].Ent.Endorsement.Val.Classes)
	doc, err := jsonshred.Compose(rows[0].Shredded)
	require.NoError(t, err)
	want, err := jsonshred.Canonical([]byte(args))
	require.NoError(t, err)
	assert.Equal(t, string(want), string(doc))

	h := NewHasher()
	written, err := h.VaultItem(1, 3, vaultFill(t, attrs))
	require.NoError(t, err)
	readBack, err := h.VaultItem(1, 3, vaultFill(t, rows[0].Shredded))
	require.NoError(t, err)
	assert.Equal(t, written, readBack, "the row read back is the row written")
	other, err := h.VaultItem(1, 4, vaultFill(t, attrs))
	require.NoError(t, err)
	assert.NotEqual(t, written, other, "the ordinal is in the item")
}

func BenchmarkLedgerItem(b *testing.B) {
	h := NewHasher()
	for b.Loop() {
		_, _ = h.LedgerItem(1, 1, func(eb *EntryEntityBuilder) {
			eb.AddEntry(Entry{Kind: "user", Role: "user", Digest: "x", LabelExposure: []string{"a"}}).AddRequestMeta(RequestMeta{Digest: "d", Messages: 3})
		})
	}
}
