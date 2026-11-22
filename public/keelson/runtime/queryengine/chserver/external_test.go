package chserver

import (
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
)

// TestDeliverSendsExternalTablesAsAForm checks the wire of external data:
// the statement in the form's query field, each table a file part under its
// name, its format and structure on the URL.
func TestDeliverSendsExternalTablesAsAForm(t *testing.T) {
	t.Parallel()
	endpoint, cap := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	eng := newEngine(t, endpoint)

	st, _, err := eng.Deliver(context.Background(), queryengine.Request{
		SQL:      "SELECT count() FROM k JOIN j USING (id)",
		Settings: map[string]string{"session_id": "s1"},
		External: []queryengine.ExternalTable{
			{Name: "k", Format: "ArrowStream", Structure: "id UInt64", Data: []byte("kbytes")},
			{Name: "j", Format: "TabSeparated", Structure: "id UInt64, s String", Data: []byte("1\tx\n")},
		},
	})
	require.NoError(t, err)
	drain(st)

	_, body, query, header := cap.get()
	assert.Equal(t, "ArrowStream", query.Get("k_format"))
	assert.Equal(t, "id UInt64", query.Get("k_structure"))
	assert.Equal(t, "TabSeparated", query.Get("j_format"))
	assert.Equal(t, "s1", query.Get("session_id"))
	assert.Empty(t, query.Get("query"), "the statement travels in the form, not the URL")

	mediaType, params, err := mime.ParseMediaType(header.Get("Content-Type"))
	require.NoError(t, err)
	require.Equal(t, "multipart/form-data", mediaType)
	mr := multipart.NewReader(strings.NewReader(body), params["boundary"])
	got := map[string]string{}
	var order []string
	for {
		p, pErr := mr.NextPart()
		if pErr == io.EOF {
			break
		}
		require.NoError(t, pErr)
		raw, _ := io.ReadAll(p)
		got[p.FormName()] = string(raw)
		order = append(order, p.FormName())
	}
	assert.Equal(t, []string{"query", "k", "j"}, order)
	assert.Equal(t, "SELECT count() FROM k JOIN j USING (id)", got["query"])
	assert.Equal(t, "kbytes", got["k"])
	assert.Equal(t, "1\tx\n", got["j"])
}

func TestDeliverRefusesAMalformedExternalTable(t *testing.T) {
	t.Parallel()
	eng := newEngine(t, "http://127.0.0.1:1")
	for name, x := range map[string]queryengine.ExternalTable{
		"name":      {Name: "a-b", Format: "ArrowStream", Structure: "x UInt8"},
		"format":    {Name: "a", Format: "Arrow Stream", Structure: "x UInt8"},
		"structure": {Name: "a", Format: "ArrowStream"},
	} {
		_, _, err := eng.Deliver(context.Background(), queryengine.Request{SQL: "SELECT 1", External: []queryengine.ExternalTable{x}})
		assert.Error(t, err, name)
	}
	_, _, err := eng.Deliver(context.Background(), queryengine.Request{SQL: "SELECT 1", External: []queryengine.ExternalTable{
		{Name: "a", Format: "ArrowStream", Structure: "x UInt8"}, {Name: "a", Format: "ArrowStream", Structure: "x UInt8"},
	}})
	assert.Error(t, err, "two tables of one name")
	_, _, err = eng.Deliver(context.Background(), queryengine.Request{SQL: "SELECT 1",
		Settings: map[string]string{"a_format": "CSV"},
		External: []queryengine.ExternalTable{{Name: "a", Format: "ArrowStream", Structure: "x UInt8"}}})
	assert.Error(t, err, "a setting naming the table's parameter")
}

// drain reads a delivered stream to its end.
func drain(st queryengine.StreamI) {
	for {
		if _, ok := st.Next(); !ok {
			break
		}
	}
	_ = st.Close()
}
