package queryengine

import (
	"errors"
	"io"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
)

func TestRequestValidate(t *testing.T) {
	t.Parallel()
	assert.Error(t, Request{}.Validate(), "a request with no statement is not runnable")
	assert.NoError(t, Request{SQL: "SELECT 1"}.Validate(),
		"a run id is optional — an engine may have nowhere to put one")
	assert.NoError(t, Request{SQL: "SELECT 1", RunID: "play-main-box-7-1"}.Validate())
	assert.Error(t, Request{SQL: "SELECT 1", RunID: "play-'main"}.Validate(),
		"an id carrying a quote would reach into the statements built around it")
	assert.Error(t, Request{SQL: "SELECT 1", RunID: "runs.*"}.Validate(),
		"an id carrying a wildcard would reach into the subject namespace")
}

func TestRowCapTerminalFor(t *testing.T) {
	t.Parallel()
	t.Run("no cap declared is complete", func(t *testing.T) {
		assert.Equal(t, runstream.TerminalComplete, RowCap{}.TerminalFor(1_000_000).State)
	})
	t.Run("throw mode is already loud", func(t *testing.T) {
		// The ClickHouse default raises instead of truncating, so a short
		// result under it cannot be a silent prefix.
		cap := RowCap{MaxResultRows: 10}
		assert.Equal(t, runstream.TerminalComplete, cap.TerminalFor(10).State)
	})
	t.Run("under the cap is complete", func(t *testing.T) {
		cap := RowCap{MaxResultRows: 10, Breaks: true}
		assert.Equal(t, runstream.TerminalComplete, cap.TerminalFor(9).State)
	})
	t.Run("exactly at the cap is reported as possibly a prefix", func(t *testing.T) {
		// The ambiguous case, deliberately called out rather than resolved:
		// a result complete at exactly the cap is indistinguishable from one
		// cut there, and a reader told "may be a prefix" can check.
		cap := RowCap{MaxResultRows: 10, Breaks: true}
		term := cap.TerminalFor(10)
		assert.Equal(t, runstream.TerminalTruncated, term.State)
		assert.Contains(t, term.Reason, "max_result_rows=10")
		assert.Contains(t, term.Reason, "may be a prefix")
	})
	t.Run("over the cap is truncated", func(t *testing.T) {
		// break stops at a block boundary, so overshooting the cap is normal.
		cap := RowCap{MaxResultRows: 10, Breaks: true}
		assert.Equal(t, runstream.TerminalTruncated, cap.TerminalFor(12).State)
	})
}

func TestSummaryProgress(t *testing.T) {
	t.Parallel()
	s := Summary{ReadRows: 3, ReadBytes: 4, WrittenRows: 5, TotalRowsToRead: 6, ElapsedNs: 7, MemoryUsage: 8}
	p := s.Progress()
	assert.Equal(t, runstream.Progress{ReadRows: 3, ReadBytes: 4, TotalRowsToRead: 6, ElapsedNs: 7, MemoryUsage: 8}, p,
		"progress is the subset both producers can see; written rows are not one of them")
}

func TestSelectMemberIsDeterministic(t *testing.T) {
	t.Parallel()
	members := []string{"a.example", "b.example", "c.example"}
	first, ok := SelectMember(members, "generation-7")
	require.True(t, ok)
	for range 20 {
		again, ok2 := SelectMember(members, "generation-7")
		require.True(t, ok2)
		assert.Equal(t, first, again, "one generation must never straddle two members (R4)")
	}
}

func TestSelectMemberIgnoresRosterOrder(t *testing.T) {
	t.Parallel()
	// A roster assembled from a map arrives in no particular order. If the
	// choice depended on that, R4's guarantee would depend on iteration luck.
	want, ok := SelectMember([]string{"a", "b", "c", "d"}, "gen")
	require.True(t, ok)
	got, ok := SelectMember([]string{"d", "c", "b", "a"}, "gen")
	require.True(t, ok)
	assert.Equal(t, want, got)
}

func TestSelectMemberEmptyPlacement(t *testing.T) {
	t.Parallel()
	member, ok := SelectMember(nil, "gen")
	assert.False(t, ok)
	assert.Empty(t, member)
}

func TestSelectMemberSpreadsAcrossTokens(t *testing.T) {
	t.Parallel()
	// Not a balancing guarantee — spreading is a consequence of tokens
	// differing. The check is only that selection is not degenerate.
	members := []string{"a", "b", "c"}
	seen := map[string]struct{}{}
	for i := range 100 {
		m, ok := SelectMember(members, "gen-"+strconv.Itoa(i))
		require.True(t, ok)
		seen[m] = struct{}{}
	}
	assert.Greater(t, len(seen), 1, "every token landing on one member would defeat the point")
}

func TestSelectMemberEmptyAffinity(t *testing.T) {
	t.Parallel()
	// No generation declared is still a deterministic choice; it simply
	// carries no cross-query promise.
	a, ok := SelectMember([]string{"x", "y"}, "")
	require.True(t, ok)
	b, ok := SelectMember([]string{"y", "x"}, "")
	require.True(t, ok)
	assert.Equal(t, a, b)
}

func TestSliceStreamYieldsDataThenTerminal(t *testing.T) {
	t.Parallel()
	st := NewSliceStream([][]byte{[]byte("ab"), []byte("cd")}, runstream.Complete(), nil)
	defer func() { _ = st.Close() }()

	var kinds []runstream.KindE
	var seqs []runstream.Seq
	for {
		f, ok := st.Next()
		if !ok {
			break
		}
		kinds = append(kinds, f.Kind)
		seqs = append(seqs, f.Seq)
	}
	assert.Equal(t, []runstream.KindE{runstream.KindData, runstream.KindData, runstream.KindTerminal}, kinds)
	assert.Equal(t, []runstream.Seq{1, 2, 3}, seqs, "sequence starts at 1 and strictly increases")
	assert.NoError(t, st.Err())
}

func TestSliceStreamCloseIsIdempotent(t *testing.T) {
	t.Parallel()
	closer := &countingCloser{}
	st := NewSliceStream(nil, runstream.Complete(), closer)
	require.NoError(t, st.Close())
	require.NoError(t, st.Close())
	assert.Equal(t, 1, closer.n, "a consumer that closes twice must not close the body twice")
}

func TestCollectReassemblesChunks(t *testing.T) {
	t.Parallel()
	st := NewReaderStream(newStringReader("0123456789"), runstream.Complete(), nil, 4)
	body, term, err := Collect(st)
	require.NoError(t, err)
	assert.Equal(t, "0123456789", string(body))
	assert.Equal(t, runstream.TerminalComplete, term.State)
}

func TestReaderStreamChunksTheBody(t *testing.T) {
	t.Parallel()
	st := NewReaderStream(newStringReader("aaaabbbbcc"), runstream.Complete(), nil, 4)
	var sizes []int
	var last runstream.Frame[[]byte]
	for {
		f, ok := st.Next()
		if !ok {
			break
		}
		last = f
		if f.Kind == runstream.KindData {
			sizes = append(sizes, len(f.Data))
		}
	}
	assert.Equal(t, []int{4, 4, 2}, sizes, "full chunks except the last")
	assert.Equal(t, runstream.KindTerminal, last.Kind, "the terminal is the last frame, always")
}

func TestReaderStreamEmptyBodyStillTerminates(t *testing.T) {
	t.Parallel()
	st := NewReaderStream(newStringReader(""), runstream.Complete(), nil, 0)
	body, term, err := Collect(st)
	require.NoError(t, err, "an empty result is a result, not an incomplete stream")
	assert.Empty(t, body)
	assert.Equal(t, runstream.TerminalComplete, term.State)
}

func TestReaderStreamReadFailureIsAFailedTerminal(t *testing.T) {
	t.Parallel()
	boom := errors.New("connection reset")
	st := NewReaderStream(&failingReader{prefix: "partial", err: boom}, runstream.Complete(), nil, 4)
	body, term, err := Collect(st)
	require.NoError(t, err, "a failed run still ends with a terminal, so it is not incomplete")
	assert.Equal(t, runstream.TerminalFailed, term.State,
		"a stream that died must not read as a complete short result")
	assert.ErrorIs(t, term.Err, boom)
	assert.Equal(t, "partial", string(body),
		"every byte that did arrive is delivered — as a prefix, never as the answer")
	assert.ErrorIs(t, st.Err(), boom)
}

func TestStreamWithoutTerminalCollectsAsIncomplete(t *testing.T) {
	t.Parallel()
	// The contract's safety net, exercised against the shape of a producer
	// that simply stopped: nothing has to go right for a partial result to
	// be recognised as partial.
	st := &headlessStream{chunks: [][]byte{[]byte("half an answer")}}
	body, _, err := Collect(st)
	assert.ErrorIs(t, err, runstream.ErrIncomplete)
	assert.Equal(t, "half an answer", string(body), "the prefix is returned, but never as the answer")
}

// headlessStream is a producer that delivers data and then stops without
// saying how the run ended — a crash, a dropped connection, or a bug.
type headlessStream struct {
	chunks [][]byte
	pos    int
	seq    runstream.Seq
}

func (inst *headlessStream) Next() (f runstream.Frame[[]byte], ok bool) {
	if inst.pos >= len(inst.chunks) {
		return
	}
	inst.seq++
	f = runstream.DataFrame(inst.seq, inst.chunks[inst.pos])
	inst.pos++
	ok = true
	return
}

func (inst *headlessStream) Err() (err error)   { return }
func (inst *headlessStream) Close() (err error) { return }

type countingCloser struct{ n int }

func (inst *countingCloser) Close() (err error) {
	inst.n++
	return
}

func newStringReader(s string) (r io.Reader) {
	r = &failingReader{prefix: s}
	return
}

// failingReader serves prefix and then either EOF or err.
type failingReader struct {
	prefix string
	err    error
	pos    int
}

func (inst *failingReader) Read(p []byte) (n int, err error) {
	if inst.pos >= len(inst.prefix) {
		err = inst.err
		if err == nil {
			err = io.EOF
		}
		return
	}
	n = copy(p, inst.prefix[inst.pos:])
	inst.pos += n
	return
}

// truncatedBody mimics Go's chunked reader on a connection that died
// mid-response: some bytes, then io.ErrUnexpectedEOF. It is a DIFFERENT
// outcome from a body whose last chunk was simply short, and the two arrive
// at this package looking alike.
type truncatedBody struct{ sent bool }

func (inst *truncatedBody) Read(p []byte) (n int, err error) {
	if !inst.sent {
		inst.sent = true
		n = copy(p, "half a result")
		return
	}
	err = io.ErrUnexpectedEOF
	return
}

// TestTransferThatBrokeIsNotComplete is the regression this package exists
// to prevent, and it once failed here: io.ReadFull reports a final short
// chunk and a transfer that died mid-chunk as the same io.ErrUnexpectedEOF,
// so a truncated response read as a complete short answer — R9's exact
// confusion, inside the code that implements R9.
func TestTransferThatBrokeIsNotComplete(t *testing.T) {
	t.Parallel()
	st := NewReaderStream(&truncatedBody{}, runstream.Complete(), nil, 64)
	body, term, err := Collect(st)
	require.NoError(t, err, "the adapter was alive to report an outcome, so there is a terminal")
	assert.Equal(t, runstream.TerminalFailed, term.State,
		"a body that died mid-transfer must not read as the whole answer")
	assert.ErrorIs(t, st.Err(), io.ErrUnexpectedEOF)
	assert.Equal(t, "half a result", string(body), "the prefix is still returned, as a prefix")
}

// TestShortFinalChunkIsComplete is the other half of the same distinction:
// a body that ends cleanly part-way through a chunk is complete, and must
// not be reported as a broken transfer.
func TestShortFinalChunkIsComplete(t *testing.T) {
	t.Parallel()
	st := NewReaderStream(newStringReader("nine char"), runstream.Complete(), nil, 64)
	body, term, err := Collect(st)
	require.NoError(t, err)
	assert.Equal(t, runstream.TerminalComplete, term.State)
	assert.Equal(t, "nine char", string(body))
	assert.NoError(t, st.Err())
}
