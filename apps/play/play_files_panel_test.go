package play

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// play_files_panel_test.go covers what the panel decides: which results it
// claims, when it re-interns one, and what a selection publishes. What it DRAWS
// is the widget's own business and is pinned by the widget's tests and its
// headless scene (ADR-0200 M1).

// emitProbe records what a panel published, in order.
type emitProbe struct {
	ids  []SignalID
	vals []any
}

func (inst *emitProbe) Emit(id SignalID, value any) {
	inst.ids = append(inst.ids, id)
	inst.vals = append(inst.vals, value)
}

func TestFilesPanelDeclaresOneChannel(t *testing.T) {
	ch := filesPanel{}.Channels()
	require.Len(t, ch, 1)
	assert.Equal(t, chMain, ch[0].ID)
	assert.True(t, ch[0].Required)
}

// Accept lifts the column contract and carries the row cursor along, so the
// panel can tell a click that echoes the signal from one that moved it.
func TestFilesPanelAcceptsAPathResult(t *testing.T) {
	claim, reason := filesPanel{}.AcceptForChannel(chMain,
		schemaWith(strField("path"), strField("content_hash")), sigWith(4))
	require.Empty(t, reason)
	k, ok := claim.(pathClaim)
	require.True(t, ok)
	assert.Equal(t, 0, k.pathCol)
	assert.Equal(t, int64(4), k.selRow)
}

func TestFilesPanelRejectsWhatItCannotBrowse(t *testing.T) {
	_, reason := filesPanel{}.AcceptForChannel(chMain, nil, sigNone())
	assert.NotEmpty(t, reason, "no result → the run-a-query empty state")

	_, reason = filesPanel{}.AcceptForChannel(chMain, schemaWith(strField("name")), sigNone())
	assert.Contains(t, reason, "`path`")
}

func filesTestDriver(t *testing.T, rec arrow.RecordBatch, executed time.Time) (*filesDriver, pathClaim) {
	t.Helper()
	k, reason := resolvePathRows(rec.Schema())
	require.Empty(t, reason)
	inst := newFilesDriver(nil)
	inst.pendingExecuted = executed
	inst.rebuild(rec, rec.Schema(), k)
	return inst, k
}

// The interning is one pass per RESULT, not per frame: the generation is what
// the browser keys its directory cache on, and a generation that moved every
// frame would drop the cache and the selection with it.
func TestFilesDriverRebuildsOnlyOnANewResult(t *testing.T) {
	rec := pathTestRec(t, pathTestCol{name: "path", str: []string{"a/b.txt"}})
	at := time.Unix(1_700_000_000, 0)
	inst, k := filesTestDriver(t, rec, at)
	require.Equal(t, uint64(1), inst.gen)

	inst.rebuild(rec, rec.Schema(), k)
	assert.Equal(t, uint64(1), inst.gen, "same schema, same result — nothing to re-intern")

	inst.pendingExecuted = at.Add(time.Second)
	inst.rebuild(rec, rec.Schema(), k)
	assert.Equal(t, uint64(2), inst.gen, "a re-run is a new result")
}

// The two signals and the split between them: a path is true of every entry, a
// row only of one a result row named.
func TestFilesDriverPublishesPathAlwaysAndRowWhenThereIsOne(t *testing.T) {
	rec := pathTestRec(t, pathTestCol{name: "path", str: []string{"a/b.txt", "c.txt"}})
	inst, k := filesTestDriver(t, rec, time.Unix(1, 0))

	// A synthesised directory: a path, and no row to point the other panels at.
	inst.st.SelectOnly("a")
	probe := &emitProbe{}
	inst.publish(k, probe)
	assert.Equal(t, []SignalID{signalSelectionKey}, probe.ids)
	assert.Equal(t, "a", probe.vals[0])

	// An entry a row named publishes both.
	inst.st.SelectOnly("c.txt")
	probe = &emitProbe{}
	inst.publish(k, probe)
	require.Equal(t, []SignalID{signalSelectionKey, signalSelection}, probe.ids)
	assert.Equal(t, int64(1), probe.vals[1])

	// Standing on the same entry publishes nothing further.
	probe = &emitProbe{}
	inst.publish(k, probe)
	assert.Empty(t, probe.ids)
}

// A selection that merely echoes the signal back does not re-emit the row, and
// a multi-selection publishes nothing at all: there is no one thing to name.
func TestFilesDriverDoesNotEchoOrPublishAMultiSelection(t *testing.T) {
	rec := pathTestRec(t, pathTestCol{name: "path", str: []string{"a.txt", "b.txt"}})
	inst, k := filesTestDriver(t, rec, time.Unix(1, 0))
	k.selRow = 1

	inst.st.SelectOnly("b.txt")
	probe := &emitProbe{}
	inst.publish(k, probe)
	assert.Equal(t, []SignalID{signalSelectionKey}, probe.ids,
		"the cursor is already there; only the path is news")

	inst.st.SelectOnly("a.txt")
	inst.st.Select("b.txt", true)
	probe = &emitProbe{}
	inst.publish(k, probe)
	assert.Empty(t, probe.ids)
}

// What the interning dropped is said, not swallowed: a browser missing entries
// looks exactly like a query that returned none of them.
func TestFilesStatusLineReportsWhatWasNotInterned(t *testing.T) {
	rec := pathTestRec(t, pathTestCol{name: "path", str: []string{"a/b.txt", "../nope"}})
	inst, _ := filesTestDriver(t, rec, time.Unix(1, 0))

	line := inst.statusLine()
	assert.Contains(t, line, "1 files")
	assert.Contains(t, line, "1 directories")
	assert.Contains(t, line, "no usable path")

	inst.fsys.dropped = 7
	assert.Contains(t, inst.statusLine(), "the browser caps at")
}
