package chserver

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/chhttp"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
)

func TestSameFormat(t *testing.T) {
	t.Parallel()
	assert.True(t, sameFormat("ArrowStream", "arrowstream"), "format names match without regard to case")
	assert.True(t, sameFormat("TSV", "TabSeparated"))
	assert.True(t, sameFormat("TSVWithNames", "TabSeparatedWithNames"))
	assert.False(t, sameFormat("TSV", "ArrowStream"))
	assert.False(t, sameFormat("Arrow", "ArrowStream"), "Arrow is the file format, not the stream")
}

// A server before 26.8 lets the statement's FORMAT clause win over the
// header and says so in its response header. The run is refused rather than
// handed to a decoder expecting another format.
func TestDeliverRefusesABodyInAnotherFormat(t *testing.T) {
	t.Parallel()
	endpoint, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(chhttp.HeaderFormat, "TSV")
		_, _ = w.Write([]byte("1\n"))
	})
	eng := newEngine(t, endpoint)

	st, _, err := eng.Deliver(context.Background(), queryengine.Request{SQL: "SELECT 1 FORMAT TSV", Format: "ArrowStream"})
	require.NoError(t, err)
	defer func() { _ = st.Close() }()

	body, term, err := queryengine.Collect(st)
	require.NoError(t, err)
	assert.Empty(t, body)
	require.Equal(t, runstream.TerminalFailed, term.State)
	assert.ErrorContains(t, term.Err, "answered in TSV, not the requested ArrowStream")
}

func TestDeliverAcceptsTheRequestedFormatInAnySpelling(t *testing.T) {
	t.Parallel()
	endpoint, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(chhttp.HeaderFormat, "TSV")
		_, _ = w.Write([]byte("1\n"))
	})
	eng := newEngine(t, endpoint)

	st, _, err := eng.Deliver(context.Background(), queryengine.Request{SQL: "SELECT 1", Format: "TabSeparated"})
	require.NoError(t, err)
	defer func() { _ = st.Close() }()
	body, term, err := queryengine.Collect(st)
	require.NoError(t, err)
	assert.Equal(t, "1\n", string(body))
	assert.Equal(t, runstream.TerminalComplete, term.State)
}
