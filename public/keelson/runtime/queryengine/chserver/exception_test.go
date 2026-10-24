package chserver

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/chhttp"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
)

// trailer builds the bytes ClickHouse appends to a body whose run failed
// mid-stream, in the shape captured from a 26.9 server.
func trailer(tag string, message string) (b []byte) {
	msg := message + "\n"
	b = []byte(exceptionMarker + tag + "\r\n" + msg + strconv.Itoa(len(msg)) + " " + tag + exceptionMarker)
	return
}

const boom = "Code: 395. DB::Exception: boom: while executing 'FUNCTION throwIf'. (FUNCTION_THROW_IF_VALUE_IS_NON_ZERO)"

func TestCutExceptionTrailerStripsTheTaggedTrailer(t *testing.T) {
	t.Parallel()
	body := append([]byte("arrow-bytes"), trailer("gobmowsfhadugxek", boom)...)

	data, message, ok := cutExceptionTrailer(body, "gobmowsfhadugxek")
	require.True(t, ok)
	assert.Equal(t, "arrow-bytes", string(data), "the rows sent before the failure stay, the trailer goes")
	assert.Equal(t, boom, string(message), "the server's own diagnostic is the point")
}

func TestCutExceptionTrailerLeavesOtherBodiesAlone(t *testing.T) {
	t.Parallel()
	tagged := append([]byte("rows"), trailer("aaaaaaaaaaaaaaaa", boom)...)
	malformed := bytes.Replace(tagged, []byte(strconv.Itoa(len(boom)+1)+" "), []byte("9999 "), 1)
	for name, tc := range map[string]struct {
		body []byte
		tag  string
	}{
		"no trailer":           {[]byte("rows"), "aaaaaaaaaaaaaaaa"},
		"empty body":           {nil, "aaaaaaaaaaaaaaaa"},
		"another tag":          {tagged, "bbbbbbbbbbbbbbbb"},
		"no tag in the header": {tagged, ""},
		"length disagrees":     {malformed, "aaaaaaaaaaaaaaaa"},
		"marker alone":         {[]byte("rows" + exceptionMarker), "aaaaaaaaaaaaaaaa"},
	} {
		_, _, ok := cutExceptionTrailer(tc.body, tc.tag)
		assert.False(t, ok, name)
	}
}

func TestDiagnosticOfFindsTheExceptionAfterTheRows(t *testing.T) {
	t.Parallel()
	rows := bytes.Repeat([]byte("19999\t0\n"), 2*maxErrorBodyBytes/8)

	// Compressed output is buffered, so the server can still answer 500
	// after rows: they come first, then the exception, with no trailer.
	plain := append(bytes.Clone(rows), boom+"\n"...)
	assert.Equal(t, boom+"\n", string(diagnosticOf(plain, "395", "aaaaaaaaaaaaaaaa")))

	tagged := append(bytes.Clone(rows), trailer("aaaaaaaaaaaaaaaa", boom)...)
	assert.Equal(t, boom, string(diagnosticOf(tagged, "395", "aaaaaaaaaaaaaaaa")))

	rejected := []byte("Code: 62. DB::Exception: Syntax error")
	assert.Equal(t, rejected, diagnosticOf(rejected, "", ""), "a body that is only the diagnostic stays as it is")
}

func TestExceptionTrailerReaderPassesLongBodiesThrough(t *testing.T) {
	t.Parallel()
	payload := bytes.Repeat([]byte("0123456789abcdef"), 3*maxTrailerBytes/16+7)

	for name, failed := range map[string]bool{"clean": false, "failed": true} {
		body := payload
		if failed {
			body = append(bytes.Clone(payload), trailer("gobmowsfhadugxek", boom)...)
		}
		for readerName, r := range map[string]io.Reader{
			"whole":    bytes.NewReader(body),
			"one byte": iotest.OneByteReader(bytes.NewReader(body)),
			// The server closes without the last chunk after a trailer,
			// which a chunked decoder reports as io.ErrUnexpectedEOF.
			"no last chunk": io.MultiReader(bytes.NewReader(body), iotest.ErrReader(io.ErrUnexpectedEOF)),
		} {
			got, err := io.ReadAll(newExceptionTrailerReader(r, "gobmowsfhadugxek"))
			assert.Equal(t, payload, got, "%s/%s", name, readerName)
			switch {
			case failed:
				assert.ErrorContains(t, err, boom, "%s/%s", name, readerName)
			case readerName == "no last chunk":
				assert.ErrorIs(t, err, io.ErrUnexpectedEOF, "%s/%s: without a trailer a broken body stays broken", name, readerName)
			default:
				assert.NoError(t, err, "%s/%s", name, readerName)
			}
		}
	}
}

func TestDeliverMidStreamExceptionIsAFailedTerminal(t *testing.T) {
	t.Parallel()
	endpoint, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(headerExceptionTag, "gobmowsfhadugxek")
		_, _ = w.Write([]byte("rows"))
		_, _ = w.Write(trailer("gobmowsfhadugxek", boom))
	})
	eng := newEngine(t, endpoint)

	st, _, err := eng.Deliver(context.Background(), queryengine.Request{SQL: "SELECT throwIf(1)"})
	require.NoError(t, err)
	defer func() { _ = st.Close() }()

	body, term, err := queryengine.Collect(st)
	require.NoError(t, err)
	assert.Equal(t, "rows", string(body), "a decoder must never be handed the trailer as payload")
	assert.Equal(t, runstream.TerminalFailed, term.State, "a 200 that ends in an exception is not a complete result")
	assert.ErrorContains(t, term.Err, boom)
}

func TestDeliverRejectionAfterRowsReportsTheException(t *testing.T) {
	t.Parallel()
	endpoint, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(chhttp.HeaderExceptionCode, "395")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write(bytes.Repeat([]byte("0\t0\n"), 2*maxErrorBodyBytes/4))
		_, _ = w.Write([]byte(boom + "\n"))
	})
	eng := newEngine(t, endpoint)

	st, _, err := eng.Deliver(context.Background(), queryengine.Request{SQL: "SELECT throwIf(1)"})
	require.NoError(t, err)
	defer func() { _ = st.Close() }()

	_, term, err := queryengine.Collect(st)
	require.NoError(t, err)
	require.Equal(t, runstream.TerminalFailed, term.State)
	assert.Equal(t, "clickhouse http 500: "+boom, term.Err.Error(), "the rows are not the diagnostic")
}
