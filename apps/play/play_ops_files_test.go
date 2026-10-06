package play

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fsbrowser"
)

// filesPane interns a small tree into the Files driver and marks the pane
// drawn.
func filesPane(t *testing.T, p *PlayApp) arrow.RecordBatch {
	t.Helper()
	rec := pathTestRec(t,
		pathTestCol{name: "path", str: []string{"src/a.go", "src/b.go", "README", ".hidden", "src/util/u.go"}},
		pathTestCol{name: "size", i: []int64{10, 300, 5, 1, 7}})
	k, reason := resolvePathRows(rec.Schema())
	require.Empty(t, reason)
	p.filesDriver.pendingExecuted = time.Unix(1, 0)
	p.filesDriver.rebuild(rec, rec.Schema(), k)
	drawnPane(p, filesPaneId, 3, rec.Schema())
	return rec
}

func TestGetFilesListsADirectoryAndAFilter(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	filesPane(t, p)

	r := queryOp[FilesReading](t, h, opGetFiles, GetFilesArgs{})
	assert.Equal(t, uint64(3), r.Drawn.ResultId)
	assert.Equal(t, int64(5), r.Files, "the dot file is interned, and hidden from listings")
	assert.Equal(t, ".", r.Dir)
	assert.Equal(t, "list", r.Mode)
	require.Len(t, r.Entries, 2)
	assert.Equal(t, "src", r.Entries[0].Path, "directories first")
	assert.True(t, r.Entries[0].Made)
	assert.Nil(t, r.Entries[0].Row)
	assert.Equal(t, int32(3), r.Entries[0].Children)
	require.NotNil(t, r.Entries[1].Row)
	assert.Equal(t, int64(2), *r.Entries[1].Row)

	r = queryOp[FilesReading](t, h, opGetFiles, GetFilesArgs{Filter: `\.go$`})
	assert.Equal(t, int32(3), r.Total, "a filter matches at any depth")
	r = queryOp[FilesReading](t, h, opGetFiles, GetFilesArgs{Dir: "src", Limit: 1})
	require.Len(t, r.Entries, 1)
	assert.Equal(t, "src/util", r.Entries[0].Path)
	assert.Equal(t, int32(2), r.More)
	require.Error(t, queryErr(t, h, opGetFiles, GetFilesArgs{Dir: "nope"}))
	require.Error(t, queryErr(t, h, opGetFiles, GetFilesArgs{Dir: "README"}))

	require.NoError(t, applyOp(t, h, opSetFilesOptions, SetFilesOptionsArgs{SortBy: strp("size"), Descending: boolp(true), Hidden: boolp(true)}))
	r = queryOp[FilesReading](t, h, opGetFiles, GetFilesArgs{Dir: "src"})
	assert.Equal(t, "size", r.SortBy)
	require.Len(t, r.Entries, 3)
	assert.Equal(t, []string{"src/util", "src/b.go", "src/a.go"}, []string{r.Entries[0].Path, r.Entries[1].Path, r.Entries[2].Path})
	r = queryOp[FilesReading](t, h, opGetFiles, GetFilesArgs{})
	assert.Len(t, r.Entries, 3, "hidden names now listed")
}

func TestSetFilesOptionsAndSelectFilesPath(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	filesPane(t, p)
	d := p.filesDriver

	for _, in := range []SetFilesOptionsArgs{{}, {Mode: strp("grid")}, {SortBy: strp("owner")}, {Dir: strp("nope")}, {Dir: strp("README"), Mode: strp("outline")}} {
		require.Error(t, applyOp(t, h, opSetFilesOptions, in), "%+v", in)
	}
	assert.Equal(t, fsbrowser.ModeList, d.mode, "a refused call applies nothing")
	require.NoError(t, applyOp(t, h, opSetFilesOptions, SetFilesOptionsArgs{Dir: strp("/src/"), Mode: strp("outline"), Filter: strp("go")}))
	assert.Equal(t, "src", d.st.Dir())
	assert.Equal(t, fsbrowser.ModeOutline, d.mode)
	assert.Equal(t, "go", d.st.Filter())

	require.NoError(t, applyOp(t, h, opSelectFilesPath, SelectFilesPathArgs{Path: "README"}))
	assert.Equal(t, []string{"README"}, d.st.Selection())
	assert.Equal(t, ".", d.st.Dir(), "the pane moves to the path's directory")
	key, writer := signalOf(t, p, signalSelectionKey)
	assert.Equal(t, "README", key)
	assert.Equal(t, "task:t", writer)
	row, _ := signalOf(t, p, signalSelection)
	assert.Equal(t, "2", row)

	require.NoError(t, applyOp(t, h, opSelectFilesPath, SelectFilesPathArgs{Path: "src/util"}))
	key, _ = signalOf(t, p, signalSelectionKey)
	assert.Equal(t, "src/util", key)
	row, _ = signalOf(t, p, signalSelection)
	assert.Equal(t, "2", row, "a made directory has no row, so selection stays")

	for _, in := range []SelectFilesPathArgs{{}, {Path: "nope"}, {Path: "."}, {Path: "README", Clear: true}} {
		require.Error(t, applyOp(t, h, opSelectFilesPath, in), "%+v", in)
	}
	require.NoError(t, applyOp(t, h, opSelectFilesPath, SelectFilesPathArgs{Clear: true}))
	assert.Empty(t, d.st.Selection())
	r := queryOp[FilesReading](t, h, opGetFiles, GetFilesArgs{})
	assert.Equal(t, "outline", r.Mode)
}

// The mode buttons and a click go through their commands as the person's
// gestures; the click's signals keep the pane as writer.
func TestFilesGesturesGoThroughTheirCommands(t *testing.T) {
	l, eng := gestureLauncher(t)
	p := l.inner
	filesPane(t, p)
	mode := "outline"
	p.filesDriver.requestOptions(SetFilesOptionsArgs{Mode: &mode})
	e := lastEntry(t, eng)
	assert.Equal(t, opSetFilesOptions, e.Op)
	assert.Equal(t, opwire.WriterPerson, e.Writer)
	assert.Equal(t, fsbrowser.ModeOutline, p.filesDriver.mode)

	p.filesDriver.st.SelectOnly("src/b.go")
	p.filesDriver.routeBrowser(fsbrowser.Result{SelectionChanged: true}, p.filesDriver.st.Dir(), fsbrowser.SortByName, false)
	e = lastEntry(t, eng)
	assert.Equal(t, opSelectFilesPath, e.Op)
	assert.Equal(t, opwire.WriterPerson, e.Writer)
	key, writer := signalOf(t, p, signalSelectionKey)
	assert.Equal(t, "src/b.go", key)
	assert.Equal(t, filesPaneId, writer)
	assert.Equal(t, "src/b.go", p.filesDriver.emitted, "the pane's own publish finds it in place")
}

// chatPane folds a three-message transcript into the Chat driver.
func chatPane(t *testing.T, p *PlayApp) {
	t.Helper()
	mem := memory.NewGoAllocator()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "ts", Type: &arrow.TimestampType{Unit: arrow.Millisecond}, Nullable: true},
		{Name: "sender", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "body", Type: arrow.BinaryTypes.String},
		{Name: "id", Type: arrow.BinaryTypes.String},
		{Name: "reply_to", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)
	b := array.NewRecordBuilder(mem, schema)
	defer b.Release()
	b.Field(0).(*array.TimestampBuilder).AppendValues([]arrow.Timestamp{3000, 1000, 2000, 4000}, nil)
	b.Field(1).(*array.StringBuilder).AppendValues([]string{"bob", "alice", "bob", ""}, []bool{true, true, true, false})
	b.Field(2).(*array.StringBuilder).AppendValues([]string{"third", "first", "second", "lost"}, nil)
	b.Field(3).(*array.StringBuilder).AppendValues([]string{"m3", "m1", "m2", "m4"}, nil)
	b.Field(4).(*array.StringBuilder).AppendValues([]string{"m1", "", "", ""}, []bool{true, false, false, false})
	rec := b.NewRecordBatch()
	t.Cleanup(rec.Release)
	k, reason := resolveChatColumns(schema)
	require.Empty(t, reason)
	p.chatDriver.rebuild(rec, 6, k, nil, nil, false)
	p.chatDriver.participantsPresent = true
	p.chatDriver.participantsReject = "the `participants` CTE needs a `sender` column (plus optional `name` and `color`)"
	drawnPane(p, chatPaneId, 6, schema)
}

func TestGetChatPaneReadsTheFoldedTranscript(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	chatPane(t, p)
	r := queryOp[ChatPaneReading](t, h, opGetChatPane, GetChatPaneArgs{})
	assert.Equal(t, uint64(6), r.Drawn.ResultId)
	assert.Equal(t, int32(3), r.Messages)
	assert.Equal(t, int32(1), r.Skipped, "the row with no sender")
	assert.Contains(t, r.Participants, "not usable: the `participants` CTE needs")
	assert.Equal(t, "absent", r.Reactions)
	require.Len(t, r.Roster, 2)
	assert.Equal(t, "alice", r.Roster[0].Sender, "in order of first appearance")
	require.Len(t, r.List, 3)
	assert.Equal(t, []string{"first", "second", "third"}, []string{r.List[0].Body, r.List[1].Body, r.List[2].Body})
	assert.Equal(t, int64(1), r.List[0].Row)
	require.NotNil(t, r.List[2].ReplyTo)
	assert.Equal(t, int32(0), *r.List[2].ReplyTo)
	assert.Equal(t, "1970-01-01 00:00:01.000", r.List[0].Time)

	r = queryOp[ChatPaneReading](t, h, opGetChatPane, GetChatPaneArgs{Limit: 1})
	require.Len(t, r.List, 1)
	assert.Equal(t, "third", r.List[0].Body, "the tail by default")
	assert.Equal(t, int32(2), r.Before)
	off := int32(1)
	r = queryOp[ChatPaneReading](t, h, opGetChatPane, GetChatPaneArgs{Offset: &off, Limit: 1})
	require.Len(t, r.List, 1)
	assert.Equal(t, "second", r.List[0].Body)
	assert.Equal(t, int32(1), r.After)
	off = 9
	require.Error(t, queryErr(t, h, opGetChatPane, GetChatPaneArgs{Offset: &off}))
	assert.Contains(t, p.chatDriver.statusLine(), "participants not joined")
}
