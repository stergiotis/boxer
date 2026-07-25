package runstream

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectorHappyPath(t *testing.T) {
	var col Collector[string]
	require.NoError(t, col.Push(DataFrame(1, "a")))
	require.NoError(t, col.Push(ProgressFrame[string](2, Progress{ReadRows: 10, TotalRowsToRead: 100})))
	require.NoError(t, col.Push(DataFrame(3, "b")))
	require.NoError(t, col.Push(TerminalFrame[string](4, Complete())))

	assert.Equal(t, []string{"a", "b"}, col.Data())
	p, ok := col.Progress()
	assert.True(t, ok)
	assert.Equal(t, uint64(10), p.ReadRows)
	assert.True(t, col.Done())

	term, err := col.Terminal()
	require.NoError(t, err)
	assert.Equal(t, TerminalComplete, term.State)
}

// TestCollectorNoTerminalIsIncomplete is the load-bearing rule: a stream
// that stops is not a stream that finished.
func TestCollectorNoTerminalIsIncomplete(t *testing.T) {
	var col Collector[string]
	require.NoError(t, col.Push(DataFrame(1, "a")))
	require.NoError(t, col.Push(DataFrame(2, "b")))

	assert.False(t, col.Done())
	_, err := col.Terminal()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrIncomplete)

	// The prefix is still readable — it is just known to be a prefix.
	assert.Equal(t, []string{"a", "b"}, col.Data())
}

func TestCollectorEmptyStreamIsIncomplete(t *testing.T) {
	var col Collector[string]
	_, err := col.Terminal()
	assert.ErrorIs(t, err, ErrIncomplete)
	assert.Empty(t, col.Data())
	_, ok := col.Progress()
	assert.False(t, ok, "no progress frame carries no meaning")
}

func TestCollectorRejectsSecondTerminal(t *testing.T) {
	var col Collector[string]
	require.NoError(t, col.Push(TerminalFrame[string](1, Complete())))

	err := col.Push(TerminalFrame[string](2, Truncated("row limit")))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "after the terminal")

	// The first terminal stands — a late frame cannot rewrite the outcome.
	term, tErr := col.Terminal()
	require.NoError(t, tErr)
	assert.Equal(t, TerminalComplete, term.State)
}

func TestCollectorRejectsFramesAfterTerminal(t *testing.T) {
	var col Collector[string]
	require.NoError(t, col.Push(DataFrame(1, "a")))
	require.NoError(t, col.Push(TerminalFrame[string](2, Complete())))

	assert.Error(t, col.Push(DataFrame(3, "late")))
	assert.Error(t, col.Push(ProgressFrame[string](4, Progress{})))
	assert.Equal(t, []string{"a"}, col.Data(), "a late frame changes nothing")
}

func TestCollectorRequiresStrictlyIncreasingSeq(t *testing.T) {
	var col Collector[string]
	require.NoError(t, col.Push(DataFrame(5, "a")))

	for _, seq := range []Seq{5, 4, 0} {
		err := col.Push(DataFrame(seq, "x"))
		require.Error(t, err, "seq=%d", seq)
		assert.Contains(t, err.Error(), "strictly increase")
	}
	require.NoError(t, col.Push(DataFrame(6, "b")), "a gap is fine, a repeat is not")
	assert.Equal(t, []string{"a", "b"}, col.Data())
}

// TestCollectorRejectsUnsetKindAndState pins the zero-value discipline: a
// frame nobody filled in must not read as a data frame, and a terminal that
// does not say how the run ended must not read as success.
func TestCollectorRejectsUnsetKindAndState(t *testing.T) {
	var col Collector[string]
	err := col.Push(Frame[string]{Seq: 1})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no kind")

	err = col.Push(Frame[string]{Seq: 1, Kind: KindTerminal})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not say how the run ended")
	assert.False(t, col.Done(), "a rejected terminal does not end the stream")
}

func TestTerminalConstructors(t *testing.T) {
	assert.Equal(t, TerminalComplete, Complete().State)

	tr := Truncated("max_result_rows=100")
	assert.Equal(t, TerminalTruncated, tr.State)
	assert.Contains(t, tr.Reason, "max_result_rows")
	assert.NoError(t, tr.Err)

	cause := errors.New("connection reset")
	f := Failed(cause)
	assert.Equal(t, TerminalFailed, f.State)
	assert.ErrorIs(t, f.Err, cause)
	assert.Contains(t, f.Reason, "connection reset", "the reason is readable without unwrapping")

	assert.Empty(t, Failed(nil).Reason)
}

func TestEnumNames(t *testing.T) {
	seen := map[string]struct{}{}
	for _, k := range AllKinds {
		require.NotEmpty(t, k.String())
		_, dup := seen[k.String()]
		assert.False(t, dup, "kind names must be distinct: %s", k)
		seen[k.String()] = struct{}{}
	}
	seen = map[string]struct{}{}
	for _, s := range AllTerminalStates {
		require.NotEmpty(t, s.String())
		_, dup := seen[s.String()]
		assert.False(t, dup, "terminal-state names must be distinct: %s", s)
		seen[s.String()] = struct{}{}
	}
	assert.Equal(t, "unknown", KindE(200).String())
	assert.Equal(t, "unknown", TerminalStateE(200).String())
}

// TestZeroValuesAreNotSuccess states the two default-deny properties
// together, since they are the reason the enums are ordered as they are.
func TestZeroValuesAreNotSuccess(t *testing.T) {
	var k KindE
	var s TerminalStateE
	assert.Equal(t, KindUnknown, k)
	assert.Equal(t, TerminalUnknown, s)
	assert.NotEqual(t, TerminalComplete, s)

	var col Collector[int]
	assert.False(t, col.Done(), "the zero collector has not finished")
}
