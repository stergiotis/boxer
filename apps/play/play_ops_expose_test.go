package play

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/introspecthttp"
	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/egui2test"
)

// exposeLauncher is a play window wired to a dataset service whose
// registry a started loopback listener serves, and a publisher's bus; the
// service attests agentCall. The window binds kernel_top__result, a
// dataset agentCall's task published.
func exposeLauncher(t *testing.T) (l *PlayLauncher, h app.OperationsHandlerI, publisher app.BusI) {
	t.Helper()
	withQueryEndpoint(t, "http://127.0.0.1:1/query")
	logger := zerolog.Nop()
	bus := inprocbus.NewInst(logger)
	reg := introspect.NewRegistry()
	svc, err := adhocdata.NewService(adhocdata.Config{Bus: bus, Registry: reg, Dir: t.TempDir(), Log: logger})
	require.NoError(t, err)
	svc.SetDispatcher(attestAll{})
	srv := introspecthttp.New(introspecthttp.Config{Registry: reg}, logger)
	require.NoError(t, srv.Start())
	t.Cleanup(func() {
		_ = svc.Close(context.Background())
		_ = srv.Stop(context.Background())
	})
	caps := []app.SubjectFilter{{Pattern: "runtime.adhoc.>", Direction: app.CapDirectionBoth, Reason: "test"}}
	l, h = opsLauncher(t)
	l.inner.client = NewClient(ClientConfig{URL: "http://ch.example:8123/"}, nil)
	l.bus, l.log = bus.NewClient("test.play", caps), logger
	l.inner.bus = l.bus
	l.ioCtx = t.Context()
	l.inner.client.SetSessionBus(l.bus)
	publisher = bus.NewClient("test.publisher", caps)
	in := adhocdata.BundlePublishInput{Alias: "kernel_top", Document: []byte("SELECT 1"), OnBehalfOf: &agentCall,
		Datasets:   []adhocdata.BundleDatasetInput{{LocalName: "result", ArrowIPCStream: intStream(t, 7, 8)}},
		Provenance: adhocdata.BundleProvenance{FromNothingSealed: true}}
	_, err = adhocdata.PublishBundleRequest(t.Context(), publisher, in)
	require.NoError(t, err)
	res, err := adhocdata.ResolveRequest(t.Context(), l.bus, "kernel_top__result")
	require.NoError(t, err)
	l.inner.client.bindDataset("top", res.Handle)
	l.inner.client.setBundleOrigin("top", "kernel_top__result", "kernel_top", "")
	return
}

func exposeOp(t *testing.T, h app.OperationsHandlerI, obo *app.OnBehalfOf, key string, args ExposeDatasetArgs) (out ExposeDatasetResult, err error) {
	t.Helper()
	call := app.OperationCall{Writer: "task:t", OnBehalfOf: obo, Key: key}
	raw := mustEncode(t, args)
	if _, err = h.ApplyCommand(call, opExposeDataset, raw); err != nil {
		return
	}
	_, result, err := settleUntil(t, h, call, opExposeDataset, raw, func(s bool) bool { return s })
	if err != nil {
		return
	}
	out, err = buscodec.Decode[ExposeDatasetResult](result)
	require.NoError(t, err)
	return
}

func getWithToken(t *testing.T, url string, token string) (status int, body []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, body
}

// An agent's expose_dataset, named by the window's name for the dataset,
// answers what a reader needs; list_datasets then says it is exposed and
// never shows the token; revoke_exposure ends it.
func TestExposeDatasetAnswersWhatAReaderNeeds(t *testing.T) {
	l, h, _ := exposeLauncher(t)
	obo := agentCall
	obo.Destinations = []string{"expose:kernel_top"}

	res, err := exposeOp(t, h, &obo, "k1", ExposeDatasetArgs{Name: "top", Minutes: 30})
	require.NoError(t, err)
	assert.Equal(t, "kernel_top__result", res.Alias, "the window's name maps to the alias")
	assert.True(t, strings.HasSuffix(res.URL, "/exposed/kernel_top__result"))
	assert.Regexp(t, `^bxe_[0-9a-f]{64}$`, res.Token)
	assert.Contains(t, res.ClickHouse, "headers('Authorization' = 'Bearer "+res.Token+"')")
	assert.Contains(t, res.Curl, res.Token)
	assert.Equal(t, uint64(2), res.Rows)
	expires, err := time.Parse(time.RFC3339, res.Expires)
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().Add(30*time.Minute), expires, time.Minute)
	status, _ := getWithToken(t, res.URL, res.Token)
	assert.Equal(t, http.StatusOK, status)
	_, made, _, _ := l.inner.exposures.read()
	assert.Contains(t, made, res.Exposure, "the window keeps the answer for the person")

	call := app.OperationCall{Writer: "task:t", OnBehalfOf: &obo}
	raw, _, err := h.Snapshot().ExternalRead(call, opListDatasets, mustEncode(t, DatasetArgs{}))
	require.NoError(t, err)
	list, err := buscodec.Decode[DatasetList](raw)
	require.NoError(t, err)
	require.Len(t, list.Exposures, 1)
	assert.Equal(t, res.Exposure, list.Exposures[0].Exposure)
	assert.True(t, list.Exposures[0].Yours)
	assert.Equal(t, uint64(1), list.Exposures[0].Reads)
	require.NotEmpty(t, list.Datasets)
	assert.True(t, list.Datasets[0].Exposed)
	assert.NotContains(t, string(raw), res.Token, "the listing never carries the token")

	rcall := app.OperationCall{Writer: "task:t", OnBehalfOf: &obo, Key: "k2"}
	rargs := mustEncode(t, RevokeExposureArgs{Exposure: res.Exposure})
	_, err = h.ApplyCommand(rcall, opRevokeExposure, rargs)
	require.NoError(t, err)
	_, rraw, err := settleUntil(t, h, rcall, opRevokeExposure, rargs, func(s bool) bool { return s })
	require.NoError(t, err)
	revoked, err := buscodec.Decode[RevokeExposureResult](rraw)
	require.NoError(t, err)
	assert.Equal(t, res.Exposure, revoked.Revoked)
	status, _ = getWithToken(t, res.URL, res.Token)
	assert.Equal(t, http.StatusNotFound, status)
}

// A task whose grant lists no expose:<prefix> for the alias is refused at
// the call with the destination to ask for; the service refuses a dataset
// the task did not publish, and the answer says who may expose it.
func TestExposeDatasetIsRefusedWithTheDestinationOrTheReason(t *testing.T) {
	_, h, publisher := exposeLauncher(t)
	obo := agentCall
	_, err := exposeOp(t, h, &obo, "k1", ExposeDatasetArgs{Name: "top"})
	var refusal *app.OperationRefusal
	require.ErrorAs(t, err, &refusal)
	assert.Equal(t, []string{"expose:kernel_top__result"}, refusal.Destinations)

	_, err = adhocdata.PublishRequest(t.Context(), publisher, adhocdata.PublishInput{Alias: "persons", ArrowIPCStream: intStream(t, 1)})
	require.NoError(t, err)
	obo.Destinations = []string{"expose:persons"}
	_, err = exposeOp(t, h, &obo, "k2", ExposeDatasetArgs{Name: "persons"})
	require.ErrorAs(t, err, &refusal)
	assert.Empty(t, refusal.Destinations)
	assert.Contains(t, refusal.Reason, "only the person exposes it")

	for _, name := range []string{"adhoc_0123456789abcdef", "no-good", ""} {
		_, err = exposeOp(t, h, &obo, "k3", ExposeDatasetArgs{Name: name})
		assert.Error(t, err, name)
	}
	_, err = exposeOp(t, h, &obo, "k4", ExposeDatasetArgs{Name: "persons", Minutes: 1441})
	assert.ErrorContains(t, err, "at most 1440 minutes")
}

// The person's Expose in the Data sets tab exposes the dataset without a
// grant and the tab draws the lines a reader pastes, headless.
func TestThePersonExposesFromTheDataSetsTab(t *testing.T) {
	t.Cleanup(egui2test.Install())
	l, _, _ := exposeLauncher(t)
	p := l.inner
	p.personExpose("top")
	require.Eventually(t, func() bool { _, made, _, _ := p.exposures.read(); return len(made) == 1 }, 2*time.Second, 10*time.Millisecond)
	_, made, _, _ := p.exposures.read()
	var res adhocdata.ExposeResult
	for _, r := range made {
		res = r
	}
	assert.Empty(t, res.Task, "the person's")
	status, body := getWithToken(t, res.CSVURL, res.Token)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, "v\n7\n8\n", string(body))
	renderHeadless(t, func() { p.renderDatasetsTab() })
	assert.Contains(t, exposureLine(res.ExposureInfo), "exposed "+res.Id)

	p.personRevoke(res.Id)
	require.Eventually(t, func() bool { _, made, _, _ := p.exposures.read(); return len(made) == 0 }, 2*time.Second, 10*time.Millisecond)
	renderHeadless(t, func() { p.renderDatasetsTab() })
}
