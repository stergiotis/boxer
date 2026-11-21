package storeexec

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/chclient"
)

func TestConnect_AnswersWithAnExecutorOrNamesTheServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("26.9.1.1\n"))
	}))
	exec, err := Connect(context.Background(), chclient.Config{URL: srv.URL + "/"})
	require.NoError(t, err)
	require.NotNil(t, exec)

	srv.Close()
	_, err = Connect(context.Background(), chclient.Config{URL: srv.URL + "/"})
	require.Error(t, err, "a server that does not answer is reported before any store is built on it")
	require.Contains(t, err.Error(), "not reachable")
}

// TestConnect_RefusesAServerBelowTheFloor: the release read in the connect
// round trip is checked, and the refusal names the floor.
func TestConnect_RefusesAServerBelowTheFloor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("25.3.2.1\n"))
	}))
	defer srv.Close()
	_, err := Connect(context.Background(), chclient.Config{URL: srv.URL + "/"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "below the floor")
}
