package play

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// recordingEndpoint answers every statement with OK and keeps what it got.
type recordingEndpoint struct {
	mu      sync.Mutex
	queries []string
	bodies  []string
}

func (inst *recordingEndpoint) serve(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		inst.mu.Lock()
		inst.queries = append(inst.queries, r.URL.Query().Get("query"))
		inst.bodies = append(inst.bodies, string(b))
		inst.mu.Unlock()
		_, _ = w.Write([]byte("0\n"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (inst *recordingEndpoint) count() int {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return len(inst.queries)
}

func TestAppStatementGoesToTheDecidedTarget(t *testing.T) {
	var ep recordingEndpoint
	srv := ep.serve(t)
	cli := NewClient(ClientConfig{URL: srv.URL + "/"}, nil)
	raw, err := cli.appStatement(context.Background(), "INSERT INTO boxer.tslabels FORMAT JSONEachRow",
		strings.NewReader(`{"a":1}`), appWriteLabel{})
	require.NoError(t, err)
	assert.Equal(t, "0\n", string(raw))
	require.Equal(t, 1, ep.count())
	assert.Equal(t, "INSERT INTO boxer.tslabels FORMAT JSONEachRow", ep.queries[0])
	assert.Equal(t, `{"a":1}`, ep.bodies[0])
}

func TestAppStatementGate(t *testing.T) {
	var ep recordingEndpoint
	srv := ep.serve(t)

	off := NewClient(ClientConfig{URL: srv.URL + "/", AppWritesOff: true}, nil)
	_, err := off.appStatement(context.Background(), pinMetaDDL, nil, appWriteLabel{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "BOXER_PLAY_APP_WRITES")

	cli := NewClient(ClientConfig{URL: srv.URL + "/"}, nil)
	_, err = cli.appStatement(context.Background(), pinMetaDDL, nil, appWriteLabel{confined: true})
	require.Error(t, err, "confined data goes only where sealed plaintext may")
	assert.Contains(t, err.Error(), "confined")

	host := endpointHost(srv.URL)
	_, err = cli.appStatement(context.Background(), pinMetaDDL, nil,
		appWriteLabel{agent: &app.OnBehalfOf{Destinations: []string{"keelson:apps"}}})
	var lim *AgentLimitError
	require.True(t, errors.As(err, &lim))
	assert.Contains(t, lim.Reason, DestinationClickHouse(host))
	assert.Equal(t, 0, ep.count(), "a refused write sends nothing")

	_, err = cli.appStatement(context.Background(), pinMetaDDL, nil,
		appWriteLabel{agent: &app.OnBehalfOf{Destinations: []string{DestinationClickHouse(host)}}})
	require.NoError(t, err)
	assert.Equal(t, 1, ep.count())
}

func TestExecuteWriteHoldsItsOwnGate(t *testing.T) {
	var ep recordingEndpoint
	srv := ep.serve(t)
	cli := NewClient(ClientConfig{URL: srv.URL + "/"}, nil)
	_, err := cli.ExecuteWrite(context.Background(), "INSERT INTO t SELECT 1", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "BOXER_PLAY_ALLOW_WRITES")
	assert.Equal(t, 0, ep.count())
}

func TestPinResultIsConsequentialAndNeedsAResult(t *testing.T) {
	m := (&PlayLauncher{}).Manifest()
	spec, ok := m.Operations.Lookup(opPinResult)
	require.True(t, ok)
	assert.Equal(t, app.OperationEffectConsequential, spec.Effect)
	assert.True(t, spec.Agents)

	_, h := opsLauncher(t)
	_, err := h.ApplyCommand(app.OperationCall{Writer: "task:t"}, opPinResult, nil)
	require.Error(t, err, "no endpoint, nothing to pin to")
}
