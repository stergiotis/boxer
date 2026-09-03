package play

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/cbor/diag"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonform"
	cwruntime "github.com/stergiotis/boxer/public/semistructured/leeway/canonwire/runtime"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonwire/streamenc"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// factsCards probes the facts record's schema through the CardDriver — the
// app's single leeway reconstruction point — as the Detail pane does.
func factsCards(t *testing.T) *CardDriver {
	t.Helper()
	rec := factsRecord(t)
	cards := NewCardDriver(c.NewWidgetIdStack(), nil)
	require.True(t, cards.EnsureFor(rec.Schema()), "a facts record is leeway-shaped")
	require.NotNil(t, cards.IR())
	return cards
}

// TestIdentity_RowMatchesPackageDigests pins the Detail strip's values to
// the packages' own: the canonform digest of a one-row slice equals the
// package's digest of that row in a whole-batch drive under the same
// options, and the canonwire fingerprint is the runtime's fingerprint of the
// stream encoder's item for that row.
func TestIdentity_RowMatchesPackageDigests(t *testing.T) {
	rec := factsRecord(t)
	cards := factsCards(t)
	comp, err := newIdentityComputer(cards.TableDesc(), cards.IR(), cards.Driver())
	require.NoError(t, err)
	require.Contains(t, comp.pin, "canonform/v1")

	// Reference: whole-batch drives through fresh encoders.
	ref, err := canonform.NewEncoder(cards.IR(), identityCanonformOptions())
	require.NoError(t, err)
	require.NoError(t, cards.Driver().DriveRecordBatch(ref, rec))
	wire, err := streamenc.NewEncoder(cards.TableDesc(), cards.IR())
	require.NoError(t, err)
	require.NoError(t, cards.Driver().DriveRecordBatch(wire, rec))
	require.Equal(t, 2, ref.NumRecords())
	require.Equal(t, 2, wire.NumEntities())

	for row := range int64(2) {
		v, err := comp.row(rec, row)
		require.NoError(t, err)
		require.Equal(t, ref.RecordDigest(int(row)), v.canon[:], "canonform digest of row %d", row)
		require.Equal(t, cwruntime.Fingerprint(wire.Entity(int(row))), v.wire, "canonwire fingerprint of row %d", row)
		require.Equal(t, len(wire.Entity(int(row))), v.wireLen)
		require.NoError(t, v.wireErr, "the wire item is canonical")
	}
	v0, _ := comp.row(rec, 0)
	v1, _ := comp.row(rec, 1)
	require.NotEqual(t, v0.canon, v1.canon, "two different rows, two digests")
	require.NotEqual(t, v0.wire, v1.wire)
}

// TestIdentity_RowItemsAreTheDigestedBytes pins the disclosure's items: the
// canonform sequence renders in diagnostic notation, the wire item passes
// the table-free checker and fingerprints to the strip's value.
func TestIdentity_RowItemsAreTheDigestedBytes(t *testing.T) {
	rec := factsRecord(t)
	cards := factsCards(t)
	comp, err := newIdentityComputer(cards.TableDesc(), cards.IR(), cards.Driver())
	require.NoError(t, err)
	v, err := comp.row(rec, 1)
	require.NoError(t, err)

	canonItems, wireItem, err := comp.rowItems(cards.IR(), rec, 1)
	require.NoError(t, err)
	require.NotEmpty(t, canonItems)
	require.NoError(t, cwruntime.VerifyCanonical(wireItem))
	require.Equal(t, v.wire, cwruntime.Fingerprint(wireItem))
	require.Equal(t, v.wireLen, len(wireItem))

	text, err := diag.String(canonItems, diag.Options{Sequence: true, Annotate: annotateCanonform})
	require.NoError(t, err)
	require.Contains(t, text, "/ leaf digests /", "the entity item's key 1 is labelled")
	text, err = diag.String(wireItem, diag.Options{TagComments: true, Annotate: annotateCanonwire})
	require.NoError(t, err)
	require.Contains(t, text, "/ version /")
	require.Contains(t, text, "/ tagged /")
	require.Contains(t, text, "/ entity-id /")
}

// TestIdentity_DetailCachesPerResultAndRow pins the strip's cache key: the
// same (result, row) is computed once; a new result or row recomputes.
func TestIdentity_DetailCachesPerResultAndRow(t *testing.T) {
	rec := factsRecord(t)
	cards := factsCards(t)
	d := newIdentityDetail(c.NewWidgetIdStack())
	v, err, ok := d.valuesFor(cards, rec, 0, 7)
	require.True(t, ok)
	require.NoError(t, err)
	again, _, _ := d.valuesFor(cards, rec, 0, 7)
	require.Equal(t, v, again)
	require.Equal(t, ResultID(7), d.result)
	other, _, _ := d.valuesFor(cards, rec, 1, 7)
	require.NotEqual(t, v.canon, other.canon)
	require.EqualValues(t, 1, d.row)
	require.False(t, d.itemsFor, "items are not computed until the disclosure opens")
	_, _, ierr := d.items(cards, rec, 1)
	require.NoError(t, ierr)
	require.True(t, d.itemsFor)
	_, _, _ = d.valuesFor(cards, rec, 1, 8)
	require.False(t, d.itemsFor, "a new result drops the items")
}
