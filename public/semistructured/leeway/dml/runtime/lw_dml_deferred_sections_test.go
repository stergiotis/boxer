package runtime_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb/ebtest"
	"github.com/stergiotis/boxer/public/semistructured/leeway/dml/runtime"
)

// The buffer's whole job is order, attribution and the frame invariants — the
// three things each write spelling used to solve for itself (ADR-0183 D4).

func TestFlushRunsSectionsFirstSeenAndContributionsInCallOrder(t *testing.T) {
	var buf runtime.DeferredSectionBuffer
	var log []string

	require.NoError(t, buf.StartKind("Label"))
	buf.Enqueue("symbol", "Label", func() error { log = append(log, "symbol/Label"); return nil })
	buf.Enqueue("u64Array", "Label", func() error { log = append(log, "u64Array/Label"); return nil })

	require.NoError(t, buf.StartKind("State"))
	// State reaches symbol second and touches no new section.
	buf.Enqueue("symbol", "State", func() error { log = append(log, "symbol/State"); return nil })

	require.Equal(t, []string{"symbol", "u64Array"}, buf.Sections())
	require.NoError(t, buf.Flush(func(section string) error {
		log = append(log, "end/"+section)
		return nil
	}))

	assert.Equal(t, []string{
		"symbol/Label", "symbol/State", "end/symbol",
		"u64Array/Label", "end/u64Array",
	}, log, "one frame per section, closed after its contributions")
}

// The invariant that keeps the entity bag an Option per kind: a second
// contribution from one component is refused where it happens, rather than
// silently marking the row un-mirrorable.
func TestStartKindRefusesASecondContributionFromOneKind(t *testing.T) {
	var buf runtime.DeferredSectionBuffer
	require.NoError(t, buf.StartKind("Label"))

	err := buf.StartKind("Label")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "one contribution per kind")
	assert.Contains(t, err.Error(), "container field", "the error says where multiplicity belongs")
}

// Raw and typed contributions are exclusive per entity, refused at whichever
// arrives second.
func TestRawAndTypedContributionsAreExclusive(t *testing.T) {
	var typedFirst runtime.DeferredSectionBuffer
	require.NoError(t, typedFirst.StartKind("Label"))
	err := typedFirst.MarkRaw()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Label", "the error names what is already on the entity")
	assert.False(t, typedFirst.IsRaw())

	var rawFirst runtime.DeferredSectionBuffer
	require.NoError(t, rawFirst.MarkRaw())
	assert.True(t, rawFirst.IsRaw())
	err = rawFirst.StartKind("Label")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exclusive")
}

// An emit failure surfaces at flush, far from the Add that supplied it, so it
// has to say which component and which section it came from.
func TestFlushAttributesAFailingContribution(t *testing.T) {
	var buf runtime.DeferredSectionBuffer
	boom := eh.Errorf("boom")

	require.NoError(t, buf.StartKind("Label"))
	buf.Enqueue("symbol", "Label", func() error { return boom })

	err := buf.Flush(nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, boom)
	txt := ebtest.Text(t, err)
	assert.Contains(t, txt, "Label")
	assert.Contains(t, txt, "symbol")
}

// A failed contribution must not leave its section open: the entity commit
// would then fail with a state-transition error naming nothing, burying the
// error that actually explains the row.
func TestFlushClosesEverySectionEvenAfterAFailure(t *testing.T) {
	var buf runtime.DeferredSectionBuffer
	var closed []string

	require.NoError(t, buf.StartKind("Label"))
	buf.Enqueue("symbol", "Label", func() error { return eh.Errorf("boom") })
	buf.Enqueue("u64Array", "Label", func() error { return nil })

	err := buf.Flush(func(section string) error {
		closed = append(closed, section)
		return nil
	})
	require.Error(t, err)
	assert.Equal(t, []string{"symbol", "u64Array"}, closed)
}

// Reset makes the buffer the next entity's, which means the previous entity's
// refusals no longer apply: the same kind may contribute again, and the raw
// spelling is available again.
func TestResetReturnsTheBufferToItsZeroState(t *testing.T) {
	var buf runtime.DeferredSectionBuffer
	require.NoError(t, buf.StartKind("Label"))
	buf.Enqueue("symbol", "Label", func() error { return nil })
	require.False(t, buf.IsEmpty())

	buf.Reset()

	assert.True(t, buf.IsEmpty())
	assert.Empty(t, buf.Sections())
	assert.False(t, buf.IsRaw())
	require.NoError(t, buf.StartKind("Label"), "a new entity may carry the kind the last one did")

	buf.Reset()
	require.NoError(t, buf.MarkRaw(), "and may be written raw where the last one was not")
}
