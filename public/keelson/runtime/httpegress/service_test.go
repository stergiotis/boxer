package httpegress

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/audit"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
)

const appId app.AppIdT = "test.http.app"

// serve is the host's arrangement: the service on one bus over specs, an
// app client holding ClientCaps for the destinations named in granted.
func serve(t *testing.T, specs []DestinationSpec, granted ...string) (cli *Client, svc *Service, sink *audit.InMemoryAuditSink) {
	t.Helper()
	bus := inprocbus.NewInst(zerolog.Nop())
	sink = audit.NewInMemoryAuditSink()
	bus.SetAuditSink(sink)
	var err error
	svc, err = NewService(bus, zerolog.Nop(), Config{Destinations: specs})
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	var caps []app.SubjectFilter
	for _, g := range granted {
		caps = append(caps, ClientCaps(g, "test: fetch")...)
	}
	cli = NewClient(bus.NewClient(appId, caps))
	cli.Timeout = 5 * time.Second
	return
}

// origin serves "hello" under /tiles/ and 404 elsewhere, and records the
// user agent it saw.
func origin(t *testing.T) (srv *httptest.Server, seenUA *string) {
	t.Helper()
	var ua string
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua = r.UserAgent()
		switch {
		case r.URL.Path == "/tiles/redirect-out":
			http.Redirect(w, r, "/elsewhere/x", http.StatusFound)
		case strings.HasPrefix(r.URL.Path, "/tiles/big"):
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(make([]byte, 2048))
		case strings.HasPrefix(r.URL.Path, "/tiles/"):
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("hello"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &ua
}

func spec(name string, d Destination) DestinationSpec {
	return DestinationSpec{Name: name, Description: "test", Resolve: func() (Destination, error) { return d, nil }}
}

func TestFetchUnderTheDestination(t *testing.T) {
	srv, ua := origin(t)
	cli, svc, sink := serve(t, []DestinationSpec{spec("tiles", Destination{Prefixes: []string{srv.URL + "/tiles/"}, UserAgent: "boxer-test/1"})}, "tiles")

	res, err := cli.Fetch(context.Background(), "tiles", Request{URL: srv.URL + "/tiles/1/2/3.png", Purpose: "test"})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.Status)
	assert.Equal(t, "text/plain", res.ContentType)
	assert.Equal(t, "hello", string(res.Body))
	assert.Equal(t, "boxer-test/1", *ua, "the destination's agent")

	calls := svc.Calls()
	require.Len(t, calls, 1)
	assert.Equal(t, appId, calls[0].Sender)
	assert.Equal(t, "tiles", calls[0].Destination)
	assert.Equal(t, 200, calls[0].Status)
	assert.Equal(t, 5, calls[0].Bytes)
	assert.False(t, calls[0].Refused)

	recs := sink.Records()
	require.NotEmpty(t, recs)
	assert.Equal(t, "net.http.fetch.tiles", recs[len(recs)-1].Subject)
	assert.Equal(t, audit.AuditResultOk, recs[len(recs)-1].Result)
}

// A non-2xx status is a completed exchange: the caller reads the status.
func TestErrorStatusIsNotAFailure(t *testing.T) {
	srv, _ := origin(t)
	cli, _, _ := serve(t, []DestinationSpec{spec("root", Destination{Prefixes: []string{srv.URL + "/"}})}, "root")
	res, err := cli.Fetch(context.Background(), "root", Request{URL: srv.URL + "/missing"})
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, res.Status)
}

func TestRefusals(t *testing.T) {
	srv, _ := origin(t)
	remote := spec("remote", Destination{Prefixes: []string{"https://tiles.example/"}})
	cli, svc, _ := serve(t, []DestinationSpec{
		spec("tiles", Destination{Prefixes: []string{srv.URL + "/tiles/"}}),
		remote,
		{Name: "broken", Resolve: func() (Destination, error) { return Destination{}, errors.New("no url configured") }},
	}, "tiles", "remote", "broken", "absent")

	cases := map[string]struct {
		dest string
		req  Request
		want string
	}{
		"outside the prefix":  {"tiles", Request{URL: srv.URL + "/other/1"}, "outside the destination"},
		"other host":          {"tiles", Request{URL: "http://example.invalid/tiles/1"}, "outside the destination"},
		"dot segment":         {"tiles", Request{URL: srv.URL + "/tiles/../other"}, "dot segment"},
		"encoded dot segment": {"tiles", Request{URL: srv.URL + "/tiles/%2e%2e/other"}, "dot segment"},
		"userinfo":            {"tiles", Request{URL: strings.Replace(srv.URL, "http://", "http://u:p@", 1) + "/tiles/1"}, "userinfo"},
		"relative":            {"tiles", Request{URL: "/tiles/1"}, "absolute"},
		"method":              {"tiles", Request{Method: http.MethodPost, URL: srv.URL + "/tiles/1"}, "not offered"},
		"confined to remote":  {"remote", Request{URL: "https://tiles.example/1", Sensitivity: queryengine.SensitivityConfined}, "sealed data"},
		"unresolved":          {"broken", Request{URL: "https://x.example/"}, "no url configured"},
		"unregistered":        {"absent", Request{URL: "https://x.example/"}, "no destination absent"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := cli.Fetch(context.Background(), c.dest, c.req)
			var refused *RefusedError
			require.True(t, errors.As(err, &refused), "%v", err)
			assert.Contains(t, refused.Reason, c.want)
		})
	}
	for _, rec := range svc.Calls() {
		assert.True(t, rec.Refused, "every case above is a refused call on the record")
	}
}

// A confined request to a loopback destination is served: the wall is
// about leaving the box.
func TestConfinedToLoopbackIsServed(t *testing.T) {
	srv, _ := origin(t)
	cli, svc, _ := serve(t, []DestinationSpec{spec("tiles", Destination{Prefixes: []string{srv.URL + "/tiles/"}})}, "tiles")
	_, err := cli.Fetch(context.Background(), "tiles", Request{URL: srv.URL + "/tiles/1", Sensitivity: queryengine.SensitivityConfined})
	require.NoError(t, err)
	assert.True(t, svc.Destinations()[0].Local)
}

// The bus enforces the grant: an app without the cap cannot put the
// request on the subject.
func TestNoCapNoRequest(t *testing.T) {
	srv, _ := origin(t)
	cli, svc, _ := serve(t, []DestinationSpec{spec("tiles", Destination{Prefixes: []string{srv.URL + "/tiles/"}})})
	_, err := cli.Fetch(context.Background(), "tiles", Request{URL: srv.URL + "/tiles/1"})
	require.ErrorIs(t, err, inprocbus.ErrPermissionViolation)
	assert.Empty(t, svc.Calls(), "the service never saw it")
}

func TestBodyCap(t *testing.T) {
	srv, _ := origin(t)
	cli, _, _ := serve(t, []DestinationSpec{spec("tiles", Destination{Prefixes: []string{srv.URL + "/tiles/"}, MaxBodyBytes: 1024})}, "tiles")
	_, err := cli.Fetch(context.Background(), "tiles", Request{URL: srv.URL + "/tiles/big"})
	require.ErrorIs(t, err, ErrTooLarge)
}

// A redirect is a second request and must stay under the prefixes.
func TestRedirectOutOfTheDestinationFails(t *testing.T) {
	srv, _ := origin(t)
	cli, _, _ := serve(t, []DestinationSpec{spec("tiles", Destination{Prefixes: []string{srv.URL + "/tiles/"}})}, "tiles")
	_, err := cli.Fetch(context.Background(), "tiles", Request{URL: srv.URL + "/tiles/redirect-out"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "outside the destination")
}

func TestTilePrefixes(t *testing.T) {
	ps, err := TilePrefixes("https://{s}.basemaps.example/light_all/{z}/{x}/{y}.png", []string{"a", "b"})
	require.NoError(t, err)
	assert.Equal(t, []string{"https://a.basemaps.example/light_all/", "https://b.basemaps.example/light_all/"}, ps)

	ps, err = TilePrefixes("http://gis:8080/tiles/{z}/{x}/{y}.png", nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"http://gis:8080/tiles/"}, ps)

	_, err = TilePrefixes("https://{s}.x.example/{z}", nil)
	require.Error(t, err)
}

func TestLocalPrefixes(t *testing.T) {
	parse := func(raws ...string) (ps []prefix) {
		for _, r := range raws {
			p, err := parsePrefix(r)
			require.NoError(t, err)
			ps = append(ps, p)
		}
		return
	}
	assert.True(t, localPrefixes(parse("http://127.0.0.1:8080/", "http://localhost/", "http://[::1]:1/")))
	assert.False(t, localPrefixes(parse("http://127.0.0.1/", "https://tile.example/")))
	assert.False(t, localPrefixes(nil))
}

func TestRegisterRejectsBadNames(t *testing.T) {
	assert.Panics(t, func() {
		Register(DestinationSpec{Name: "has.dot", Resolve: func() (Destination, error) { return Destination{}, nil }})
	})
	assert.Panics(t, func() { Register(DestinationSpec{Name: "noresolve"}) })
}
