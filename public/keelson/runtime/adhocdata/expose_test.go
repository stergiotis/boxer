package adhocdata

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/introspecthttp"
)

// syncBuffer is a log sink the listener's and the service's goroutines
// share.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (inst *syncBuffer) Write(p []byte) (int, error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.b.Write(p)
}

func (inst *syncBuffer) String() string {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.b.String()
}

// exposeRig is a dataset service whose registry a started loopback
// listener serves, with every log line and exposure record kept.
type exposeRig struct {
	svc  *Service
	srv  *introspecthttp.Server
	bus  *inprocbus.Inst
	logs *syncBuffer

	mu   sync.Mutex
	recs []ExposureAuditRecord
}

func newExposeRig(t *testing.T) (r *exposeRig) {
	t.Helper()
	r = &exposeRig{logs: &syncBuffer{}}
	logger := zerolog.New(r.logs)
	reg := introspect.NewRegistry()
	r.bus = inprocbus.NewInst(logger)
	svc, err := NewService(Config{Bus: r.bus, Registry: reg, Dir: t.TempDir(), Log: logger})
	require.NoError(t, err)
	r.svc = svc
	captureAudits(svc)
	svc.setExposureHook(func(rec ExposureAuditRecord) {
		r.mu.Lock()
		r.recs = append(r.recs, rec)
		r.mu.Unlock()
	})
	r.srv = introspecthttp.New(introspecthttp.Config{Registry: reg}, logger)
	require.NoError(t, r.srv.Start(t.Context()))
	t.Cleanup(func() {
		_ = svc.Close(context.Background())
		_ = r.srv.Stop(context.Background())
	})
	return
}

func (inst *exposeRig) records() (recs []ExposureAuditRecord) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return append([]ExposureAuditRecord(nil), inst.recs...)
}

func (inst *exposeRig) actions() (out []string) {
	for _, r := range inst.records() {
		out = append(out, r.Action+":"+r.Outcome)
	}
	return
}

// fetch GETs (or HEADs) url with token, answering status, body and ETag.
func fetch(t *testing.T, method string, url string, token string) (status int, body []byte, etag string) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	require.NoError(t, err)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("User-Agent", "grader/1.0")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, body, resp.Header.Get("ETag")
}

// The person exposes a dataset: the answer carries the token, the URL, the
// structure and the lines a reader pastes; a reader with the token gets the
// stream as sealed, and the catalog and the trail never show the token.
func TestThePersonExposesADatasetAndAReaderFetchesIt(t *testing.T) {
	r := newExposeRig(t)
	stream := int64Stream(t, false, 1, 2, 3)
	_, err := r.svc.Publish(PublishInput{Alias: "result", ArrowIPCStream: stream, By: windowA})
	require.NoError(t, err)

	res, err := r.svc.Expose(t.Context(), ExposeInput{Alias: "result", By: windowB})
	require.NoError(t, err)
	assert.Regexp(t, `^bxe_[0-9a-f]{64}$`, res.Token, "256 random bits")
	assert.Regexp(t, `^exp_[0-9a-f]{16}$`, res.Id)
	assert.Equal(t, r.srv.BaseURL()+"/exposed/result", res.URL)
	assert.Equal(t, res.URL+"/csv", res.CSVURL)
	assert.Equal(t, "`v` Int64", res.Structure)
	assert.Equal(t, uint64(3), res.Rows)
	assert.Equal(t, uint64(len(stream)), res.Bytes)
	assert.Equal(t, uint64(1), res.Revision)
	assert.WithinDuration(t, time.Now().Add(DefaultExposureLifetime), res.ExpiresAt, 5*time.Second)
	assert.Equal(t, "url('"+res.URL+"', 'ArrowStream', '`v` Int64', headers('Authorization' = 'Bearer "+res.Token+"'))", res.ClickHouse)
	assert.Equal(t, "curl -sf -H 'Authorization: Bearer "+res.Token+"' '"+res.URL+"'", res.Curl)
	assert.True(t, res.Yours)

	status, body, etag := fetch(t, http.MethodGet, res.URL, res.Token)
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, []int64{1, 2, 3}, int64Values(t, body))
	assert.Equal(t, `"result-r1"`, etag)
	status, body, _ = fetch(t, http.MethodHead, res.URL, res.Token)
	assert.Equal(t, http.StatusOK, status)
	assert.Empty(t, body)
	status, body, _ = fetch(t, http.MethodGet, res.CSVURL, res.Token)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, "v\n1\n2\n3\n", string(body))

	list, err := r.svc.Exposures(t.Context(), windowB, nil)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, uint64(2), list[0].Reads, "two reads received data; a HEAD is not a read")
	assert.Equal(t, res.Id, list[0].Id)
	assert.True(t, list[0].Yours)
	other, err := r.svc.Exposures(t.Context(), windowA, nil)
	require.NoError(t, err)
	assert.False(t, other[0].Yours, "another window did not make it")

	recs := r.records()
	assert.Equal(t, []string{"expose:ok", "read:ok", "read:ok"}, r.actions())
	assert.Equal(t, "grader/1.0", recs[1].UserAgent, "what the request declared about itself")
	assert.Equal(t, uint64(len(stream)), recs[1].Bytes)
	assert.Equal(t, "arrowstream", recs[1].Format)
	assert.Equal(t, "csv", recs[2].Format)
	assert.Equal(t, uint64(1), recs[1].Revision, "a read records the revision it served")
	for _, rec := range recs {
		assert.NotContains(t, fmt.Sprintf("%+v", rec), res.Token, "no record holds the token")
	}
	assert.NotContains(t, fmt.Sprintf("%+v", list), res.Token, "the catalog never shows the token")
	assert.NotContains(t, r.logs.String(), res.Token, "nothing logs the token")
}

// Every request that opens nothing is answered alike: no token, a wrong one,
// one for another alias, one revoked; none confirms the token existed.
func TestReadersWithoutTheTokenAreToldNothing(t *testing.T) {
	r := newExposeRig(t)
	_, err := r.svc.Publish(PublishInput{Alias: "result", ArrowIPCStream: int64Stream(t, false, 1), By: windowA})
	require.NoError(t, err)
	_, err = r.svc.Publish(PublishInput{Alias: "other", ArrowIPCStream: int64Stream(t, false, 2), By: windowA})
	require.NoError(t, err)
	res, err := r.svc.Expose(t.Context(), ExposeInput{Alias: "result", By: windowA})
	require.NoError(t, err)
	base := r.srv.BaseURL() + "/exposed/"

	answers := map[string][]byte{}
	for name, c := range map[string][2]string{
		"no token":      {base + "result", ""},
		"wrong token":   {base + "result", "bxe_" + strings.Repeat("0", 64)},
		"another alias": {base + "other", res.Token},
		"never exposed": {base + "nothing", res.Token},
	} {
		status, body, _ := fetch(t, http.MethodGet, c[0], c[1])
		assert.Equal(t, http.StatusNotFound, status, name)
		answers[name] = body
	}
	require.NoError(t, r.svc.RevokeExposure(t.Context(), res.Id, windowA, nil))
	status, body, _ := fetch(t, http.MethodGet, base+"result", res.Token)
	assert.Equal(t, http.StatusNotFound, status, "revoked")
	for name, b := range answers {
		assert.Equal(t, body, b, name)
	}
	assert.Equal(t, []string{"expose:ok", "revoke:ok"}, r.actions(), "no failed request opened or counted anything")
}

// An exposure lapses at the end of its lifetime, when its dataset is
// retracted, and when the window that made it closes; each is on the
// trail once, and a reader is told nothing afterwards.
func TestAnExposureLapses(t *testing.T) {
	r := newExposeRig(t)
	pub, err := r.svc.Publish(PublishInput{Alias: "result", ArrowIPCStream: int64Stream(t, false, 1), By: windowA})
	require.NoError(t, err)

	short, err := r.svc.Expose(t.Context(), ExposeInput{Alias: "result", By: windowA, Lifetime: 50 * time.Millisecond})
	require.NoError(t, err)
	byWindow, err := r.svc.Expose(t.Context(), ExposeInput{Alias: "result", By: windowB})
	require.NoError(t, err)
	retracted, err := r.svc.Expose(t.Context(), ExposeInput{Alias: "result", By: windowA})
	require.NoError(t, err)

	require.Eventually(t, func() bool { list, _ := r.svc.Exposures(t.Context(), windowA, nil); return len(list) == 2 }, 2*time.Second, 10*time.Millisecond)
	status, _, _ := fetch(t, http.MethodGet, short.URL, short.Token)
	assert.Equal(t, http.StatusNotFound, status, "past its lifetime")

	assert.Equal(t, 1, r.svc.endExposuresBy(t.Context(), windowB))
	status, _, _ = fetch(t, http.MethodGet, byWindow.URL, byWindow.Token)
	assert.Equal(t, http.StatusNotFound, status, "its window closed")

	require.NoError(t, r.svc.Retract(pub.Handle, windowA))
	status, _, _ = fetch(t, http.MethodGet, retracted.URL, retracted.Token)
	assert.Equal(t, http.StatusNotFound, status, "its dataset was retracted")
	require.Eventually(t, func() bool { list, _ := r.svc.Exposures(t.Context(), windowA, nil); return len(list) == 0 }, 2*time.Second, 10*time.Millisecond)

	reasons := map[string]string{}
	for _, rec := range r.records() {
		if rec.Action == ExposureAuditLapse {
			_, seen := reasons[rec.Exposure]
			assert.False(t, seen, "a lapse is recorded once")
			reasons[rec.Exposure] = rec.Reason
		}
	}
	assert.Equal(t, map[string]string{short.Id: "its lifetime ended", byWindow.Id: "the window that made it closed",
		retracted.Id: "the dataset was retracted"}, reasons)
}

// An exposure names the alias: a republish with the same columns is served
// at its new revision; one that changes the columns ends it, since the
// structure a reader was handed no longer fits.
func TestARepublishIsServedUntilItsColumnsChange(t *testing.T) {
	r := newExposeRig(t)
	_, err := r.svc.PublishBundle(t.Context(), BundlePublishInput{Alias: "sales", Document: []byte(testDoc), Datasets: twoDatasets(t), By: windowA})
	require.NoError(t, err)
	res, err := r.svc.Expose(t.Context(), ExposeInput{Alias: "sales__orders", By: windowA})
	require.NoError(t, err)
	assert.Equal(t, "sales", res.Bundle)

	again := twoDatasets(t)
	again[0].ArrowIPCStream = int64Stream(t, false, 4, 5)
	_, err = r.svc.PublishBundle(t.Context(), BundlePublishInput{Alias: "sales", Document: []byte(testDoc), Datasets: again, By: windowA})
	require.NoError(t, err)
	status, body, etag := fetch(t, http.MethodGet, res.URL, res.Token)
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, []int64{4, 5}, int64Values(t, body))
	assert.Equal(t, `"sales__orders-r2"`, etag, "the revision served")
	list, err := r.svc.Exposures(t.Context(), windowA, nil)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, uint64(2), list[0].Revision)

	again[0].ArrowIPCStream = int64Stream(t, true, 6)
	_, err = r.svc.PublishBundle(t.Context(), BundlePublishInput{Alias: "sales", Document: []byte(testDoc), Datasets: again, By: windowA})
	require.NoError(t, err)
	status, _, _ = fetch(t, http.MethodGet, res.URL, res.Token)
	assert.Equal(t, http.StatusNotFound, status, "Nullable(Int64) is not the structure handed out")
	recs := r.records()
	assert.Equal(t, ExposureAuditLapse, recs[len(recs)-1].Action)
	assert.Equal(t, "a republish changed its columns", recs[len(recs)-1].Reason)
}

// Without a listener serving the registry nothing could read an exposure,
// so none is made; nor one of a dataset that is not live, nor one longer
// than the bound.
func TestAnExposureNeedsAListenerALiveDatasetAndABoundedLifetime(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.Publish(PublishInput{Alias: "result", ArrowIPCStream: int64Stream(t, false, 1), By: windowA})
	require.NoError(t, err)
	_, err = svc.Expose(t.Context(), ExposeInput{Alias: "result", By: windowA})
	assert.ErrorIs(t, err, ErrNoListener)

	r := newExposeRig(t)
	_, err = r.svc.Expose(t.Context(), ExposeInput{Alias: "nothing", By: windowA})
	assert.ErrorIs(t, err, ErrNoLiveDataset)
	_, err = r.svc.Publish(PublishInput{Alias: "result", ArrowIPCStream: int64Stream(t, false, 1), By: windowA})
	require.NoError(t, err)
	_, err = r.svc.Expose(t.Context(), ExposeInput{Alias: "result", By: windowA, Lifetime: MaxExposureLifetime + time.Second})
	assert.ErrorContains(t, err, "at most 24h0m0s")
	for range MaxExposures {
		_, err = r.svc.Expose(t.Context(), ExposeInput{Alias: "result", By: windowA})
		require.NoError(t, err)
	}
	_, err = r.svc.Expose(t.Context(), ExposeInput{Alias: "result", By: windowA})
	assert.ErrorContains(t, err, "as many exposures as it admits")
}

// An agent's exposure is attested, needs expose:<prefix> in its task's
// grant, and is of a dataset the task's own call published at TLP:CLEAR;
// a refusal names the destination to ask for.
func TestAnAgentsExposureIsHeldToItsGrantAndItsOwnWork(t *testing.T) {
	r := newExposeRig(t)
	d := &fakeDispatcher{call: agentCall()}
	r.svc.SetDispatcher(d)
	obo := oboOf(d.call)
	_, err := r.svc.PublishBundle(t.Context(), BundlePublishInput{Alias: "sales", Document: []byte(testDoc), Datasets: twoDatasets(t),
		By: windowA, OnBehalfOf: obo, Provenance: BundleProvenance{FromNothingSealed: true}})
	require.NoError(t, err)
	_, err = r.svc.Publish(PublishInput{Alias: "persons", ArrowIPCStream: int64Stream(t, false, 1), By: windowB})
	require.NoError(t, err)

	_, err = r.svc.Expose(t.Context(), ExposeInput{Alias: "sales__orders", By: windowB, OnBehalfOf: obo})
	assert.ErrorIs(t, err, ErrUnattested, "the call was routed to another window")

	_, err = r.svc.Expose(t.Context(), ExposeInput{Alias: "sales__orders", By: windowA, OnBehalfOf: obo})
	var ge *GrantError
	require.ErrorAs(t, err, &ge)
	assert.Equal(t, "expose:sales__orders", ge.Destination)

	d.grants = []string{"expose:sales"}
	res, err := r.svc.Expose(t.Context(), ExposeInput{Alias: "sales__orders", By: windowA, OnBehalfOf: obo})
	require.NoError(t, err, "a prefix of the alias covers it")
	assert.Equal(t, "task-1", res.Task)

	d.grants = []string{"expose:persons"}
	_, err = r.svc.Expose(t.Context(), ExposeInput{Alias: "persons", By: windowA, OnBehalfOf: obo})
	assert.ErrorIs(t, err, ErrNotTheTasks, "the person published it")

	recs := r.records()
	require.Len(t, recs, 4)
	assert.Equal(t, auditDenied, recs[0].Outcome)
	assert.Equal(t, auditDenied, recs[1].Outcome)
	assert.Equal(t, auditOk, recs[2].Outcome)
	require.True(t, recs[2].Context.Has)
	assert.Equal(t, "conv-9", recs[2].Context.Val.Conversation, "the exposure is attributed to the call that made it")
	assert.Equal(t, auditDenied, recs[3].Outcome)
}

// A dataset published, or an exposure asked for, under a label above
// TLP:CLEAR is not a task's to expose.
func TestATaskDoesNotExposeAboveClear(t *testing.T) {
	r := newExposeRig(t)
	d := &fakeDispatcher{call: agentCall(), grants: []string{"expose:sales"}}
	d.call.Tier = uint8(agentrun.TierAmber)
	r.svc.SetDispatcher(d)
	obo := oboOf(d.call)
	_, err := r.svc.PublishBundle(t.Context(), BundlePublishInput{Alias: "sales", Document: []byte(testDoc), Datasets: twoDatasets(t),
		By: windowA, OnBehalfOf: obo, Provenance: BundleProvenance{FromNothingSealed: true}})
	require.NoError(t, err)
	_, err = r.svc.Expose(t.Context(), ExposeInput{Alias: "sales__orders", By: windowA, OnBehalfOf: obo})
	assert.ErrorContains(t, err, "labelled AMBER")
	_, err = r.svc.Expose(t.Context(), ExposeInput{Alias: "sales__orders", By: windowA})
	assert.NoError(t, err, "the person exposes it")
}

// The window or task that made an exposure revokes it, as does the person
// from another window of the app; another app, or another task, does not.
func TestWhoRevokesAnExposure(t *testing.T) {
	r := newExposeRig(t)
	d := &fakeDispatcher{call: agentCall(), grants: []string{"expose:sales__orders"}}
	r.svc.SetDispatcher(d)
	obo := oboOf(d.call)
	_, err := r.svc.PublishBundle(t.Context(), BundlePublishInput{Alias: "sales", Document: []byte(testDoc), Datasets: twoDatasets(t),
		By: windowA, OnBehalfOf: obo, Provenance: BundleProvenance{FromNothingSealed: true}})
	require.NoError(t, err)
	res, err := r.svc.Expose(t.Context(), ExposeInput{Alias: "sales__orders", By: windowA, OnBehalfOf: obo})
	require.NoError(t, err)
	err = r.svc.RevokeExposure(t.Context(), res.Id, Identity{App: "other.app", Instance: 9}, nil)
	assert.ErrorContains(t, err, "only the window or the task")
	assert.NoError(t, r.svc.RevokeExposure(t.Context(), res.Id, windowA, obo), "its task's call")
	assert.ErrorIs(t, r.svc.RevokeExposure(t.Context(), res.Id, windowA, obo), ErrUnknownExposure)

	res, err = r.svc.Expose(t.Context(), ExposeInput{Alias: "sales__orders", By: windowA})
	require.NoError(t, err)
	assert.NoError(t, r.svc.RevokeExposure(t.Context(), res.Id, windowB, nil), "the person, from another window of the app")
}

// An exposure serves at most MaxExposureReads requests at once; one more
// presenting the token is told to retry, not that the token is unknown.
func TestAnExposureBoundsItsOpenReads(t *testing.T) {
	r := newExposeRig(t)
	_, err := r.svc.Publish(PublishInput{Alias: "result", ArrowIPCStream: int64Stream(t, false, 1), By: windowA})
	require.NoError(t, err)
	res, err := r.svc.Expose(t.Context(), ExposeInput{Alias: "result", By: windowA})
	require.NoError(t, err)
	var open []introspect.ExposedRead
	for range MaxExposureReads {
		rd, status := r.svc.OpenExposed(t.Context(), "result", res.Token, introspect.ExposedRequest{})
		require.Equal(t, introspect.ExposedStatusOpen, status)
		open = append(open, rd)
	}
	_, status := r.svc.OpenExposed(t.Context(), "result", res.Token, introspect.ExposedRequest{})
	assert.Equal(t, introspect.ExposedStatusBusy, status)
	open[0].Done(0, nil)
	rd, status := r.svc.OpenExposed(t.Context(), "result", res.Token, introspect.ExposedRequest{})
	assert.Equal(t, introspect.ExposedStatusOpen, status)
	rd.Done(0, nil)
	for _, o := range open[1:] {
		o.Done(0, nil)
	}
	_, status = r.svc.OpenExposed(t.Context(), "result", "bxe_nope", introspect.ExposedRequest{})
	assert.Equal(t, introspect.ExposedStatusNotFound, status)
}

// Over the bus: the window asking is the envelope's, the token comes back
// once, the list never carries it, and a grant refusal names its
// destination.
func TestExposureOverTheBus(t *testing.T) {
	r := newExposeRig(t)
	caps := []app.SubjectFilter{{Pattern: "runtime.adhoc.>", Direction: app.CapDirectionBoth, Reason: "test"}}
	win := r.bus.NewClient(windowA.App, caps)
	win.SetInstanceKey(windowA.Instance)
	_, err := PublishRequest(t.Context(), win, PublishInput{Alias: "result", ArrowIPCStream: int64Stream(t, false, 1, 2)})
	require.NoError(t, err)

	res, err := ExposeRequest(t.Context(), win, ExposeInput{Alias: "result", Lifetime: 10 * time.Minute})
	require.NoError(t, err)
	assert.Regexp(t, `^bxe_[0-9a-f]{64}$`, res.Token)
	assert.Contains(t, res.ClickHouse, res.Token)
	assert.Equal(t, windowA, res.By, "the envelope's window, not a field the caller filled")
	assert.WithinDuration(t, time.Now().Add(10*time.Minute), res.ExpiresAt, 5*time.Second)

	list, err := ExposuresRequest(t.Context(), win, nil)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.True(t, list[0].Yours)
	assert.Equal(t, res.URL, list[0].URL)

	d := &fakeDispatcher{call: agentCall()}
	r.svc.SetDispatcher(d)
	_, err = ExposeRequest(t.Context(), win, ExposeInput{Alias: "result", OnBehalfOf: oboOf(d.call)})
	var ge *GrantError
	require.ErrorAs(t, err, &ge)
	assert.Equal(t, "expose:result", ge.Destination)
	_, err = ExposeRequest(t.Context(), win, ExposeInput{Alias: "nothing"})
	assert.ErrorIs(t, err, ErrNoLiveDataset)

	require.NoError(t, RevokeExposureRequest(t.Context(), win, res.Id, nil))
	list, err = ExposuresRequest(t.Context(), win, nil)
	require.NoError(t, err)
	assert.Empty(t, list)
}

// Two exposures of one dataset are independent: each has its own token, and
// revoking one leaves the other readable.
func TestEachExposureHasItsOwnToken(t *testing.T) {
	r := newExposeRig(t)
	_, err := r.svc.Publish(PublishInput{Alias: "result", ArrowIPCStream: int64Stream(t, false, 1), By: windowA})
	require.NoError(t, err)
	a, err := r.svc.Expose(t.Context(), ExposeInput{Alias: "result", By: windowA})
	require.NoError(t, err)
	b, err := r.svc.Expose(t.Context(), ExposeInput{Alias: "result", By: windowA})
	require.NoError(t, err)
	assert.NotEqual(t, a.Token, b.Token)
	assert.NotEqual(t, a.Id, b.Id)
	require.NoError(t, r.svc.RevokeExposure(t.Context(), a.Id, windowA, nil))
	status, _, _ := fetch(t, http.MethodGet, b.URL, b.Token)
	assert.Equal(t, http.StatusOK, status)
	status, _, _ = fetch(t, http.MethodGet, a.URL, a.Token)
	assert.Equal(t, http.StatusNotFound, status)
}

// Closing the service ends every exposure.
func TestClosingTheServiceEndsItsExposures(t *testing.T) {
	r := newExposeRig(t)
	_, err := r.svc.Publish(PublishInput{Alias: "result", ArrowIPCStream: int64Stream(t, false, 1), By: windowA})
	require.NoError(t, err)
	res, err := r.svc.Expose(t.Context(), ExposeInput{Alias: "result", By: windowA})
	require.NoError(t, err)
	require.NoError(t, r.svc.Close(t.Context()))
	status, _, _ := fetch(t, http.MethodGet, res.URL, res.Token)
	assert.Equal(t, http.StatusNotFound, status)
	recs := r.records()
	assert.Equal(t, "the host stopped", recs[len(recs)-1].Reason)
}

// On a host the organisation manages, an exposure hands sealed data, at the
// confined tier, to a program outside boxer: above the export clearance it
// is refused, the person's included; a clearance at the confined tier
// admits it.
func TestAManagedHostRefusesAnExposureAboveTheExportClearance(t *testing.T) {
	r := newExposeRig(t)
	_, err := r.svc.Publish(PublishInput{Alias: "result", ArrowIPCStream: int64Stream(t, false, 1), By: windowA})
	require.NoError(t, err)
	r.svc.cell.Managed = true
	_, err = r.svc.Expose(t.Context(), ExposeInput{Alias: "result", By: windowA})
	require.ErrorIs(t, err, ErrManagedHost)
	assert.ErrorContains(t, err, "AMBER above GREEN")
	recs := r.records()
	assert.Equal(t, auditDenied, recs[len(recs)-1].Outcome)
	r.svc.cell.Clearances.Export = agentrun.TierAmber
	_, err = r.svc.Expose(t.Context(), ExposeInput{Alias: "result", By: windowA})
	assert.NoError(t, err)
}
