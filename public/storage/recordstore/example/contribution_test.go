package example

import (
	"context"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
	"github.com/stergiotis/boxer/public/storage/recordstore/example/internal/lowlevel"
)

// A contribution (gen.Input.Contributions) shares a section's frame with a
// component on one entity: both attributes land in the section, the
// component still decodes, and a contribution kind is refused twice like
// a component is.
func TestAContributionSharesASectionWithAComponent(t *testing.T) {
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	ctx := context.Background()
	st := NewRevStore(exec, nil, RevStoreConfig{})
	defer st.Close()
	require.NoError(t, st.EnsureTable(ctx))
	b := st.Begin("r1", 1, RevEnvelope{}).AddRevNote(RevNote{Text: "typed"})
	b.StartContribution("extra").ContributeNote("extra", func(sec *lowlevel.InEntityRevTableSectionNote) error {
		sec.BeginAttribute("hand").AddMembershipLowCardVerbatim([]byte("extra")).EndAttribute()
		return nil
	})
	require.NoError(t, b.Commit())
	_, err = st.Flush(ctx)
	require.NoError(t, err)

	var n int
	for ent, serr := range st.ScanRevNote(ctx, recordstore.ScanOpts{}) {
		require.NoError(t, serr)
		assert.Equal(t, "typed", ent.RevNote.Val.Text, "the component still decodes beside the contribution")
		n++
	}
	assert.Equal(t, 1, n)
	for rec, qerr := range exec.QueryArrow(ctx, "SELECT length(`tv:note:value:val:s:4:::0::data`) FROM rev") {
		require.NoError(t, qerr)
		assert.EqualValues(t, 2, rec.Column(0).(*array.Uint64).Value(0), "both attributes are in the section")
		rec.Release()
	}

	twice := st.Begin("r1", 2, RevEnvelope{}).StartContribution("extra").StartContribution("extra")
	assert.Error(t, twice.Commit(), "one contribution per kind")
}
