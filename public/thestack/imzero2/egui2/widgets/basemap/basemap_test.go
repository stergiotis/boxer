package basemap

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConfigured pins the switch play's Map panel keys its basemap-on default
// on, and the one the TLS knobs are gated behind. Since BOXER_MAP_TILE_URL
// gained an OpenStreetMap default it reports "the deployment named its own
// server", not "a URL exists" — Get() always answers the latter now. Unset
// must stay false, or play starts fetching public tiles unasked and a stray
// insecure flag would apply to OpenStreetMap.
func TestConfigured(t *testing.T) {
	if Configured() {
		t.Fatalf("Configured() = true with BOXER_MAP_TILE_URL unset; the OSM default must not read as operator intent")
	}
	if TileURL.Get() == "" {
		t.Fatalf("TileURL.Get() = empty with BOXER_MAP_TILE_URL unset; want the OSM default")
	}
	TileURL.SetForTest(t, "   ") // whitespace-only trims to empty → unset
	if Configured() {
		t.Fatalf("Configured() = true with whitespace-only BOXER_MAP_TILE_URL")
	}
	TileURL.SetForTest(t, "http://mygis/{z}/{x}/{y}.png")
	if !Configured() {
		t.Fatalf("Configured() = false with BOXER_MAP_TILE_URL set")
	}
}

// TestOpenStreetMapDefaults pins the defaults against the values the retired walkers binding
// built-in source hard-codes (sources/openstreetmap.rs, and the TileSource
// trait's own tile_size/max_zoom). They are the whole point of expressing the
// default server as env params: if they drift from what the renderer's
// fallback would have produced, an unconfigured deployment silently changes
// which tiles it fetches.
func TestOpenStreetMapDefaults(t *testing.T) {
	cases := []struct {
		name, got, want string
	}{
		{"BOXER_MAP_TILE_URL", TileURL.Get(), "https://tile.openstreetmap.org/{z}/{x}/{y}.png"},
		{"BOXER_MAP_TILE_ATTRIBUTION", TileAttribution.Get(), "OpenStreetMap contributors"},
		{"BOXER_MAP_TILE_ATTRIBUTION_URL", TileAttributionURL.Get(), "https://www.openstreetmap.org/copyright"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s default = %q; want %q", tc.name, tc.got, tc.want)
		}
	}
	// hard-coded (its TileSource::max_zoom default), and what OSM actually serves.
	if zoom, set := clampMaxZoom(TileMaxZoom.Get()); !set || zoom != 19 {
		t.Errorf("BOXER_MAP_TILE_MAX_ZOOM default = (%d, %t); want (19, true)", zoom, set)
	}
}

// TestTLSKnobsDefaultToVerifiedPublicRoots pins the safe default: with nothing
// set, neither TLS knob is on, so a basemap fetches under ordinary certificate
// verification. This covers the specs themselves; the BOXER_MAP_TILE_URL
// gating on top of them is TestPortolanLoaderGatesOnConfiguredURL below.
func TestTLSKnobsDefaultToVerifiedPublicRoots(t *testing.T) {
	if TileInsecureTLS.Get() {
		t.Fatalf("BOXER_MAP_TILE_INSECURE_TLS defaults to true; verification must be on unless asked")
	}
	if TileCAFile.Get() != "" {
		t.Fatalf("BOXER_MAP_TILE_CA_FILE defaults to %q; want empty", TileCAFile.Get())
	}

	// A CA file is a path, not the PEM itself — it is read renderer-side, once
	// per tile-source construction, because the map widget reads every
	// frame.
	TileCAFile.SetForTest(t, "/etc/ssl/gis-ca.pem")
	if got := TileCAFile.Get(); got != "/etc/ssl/gis-ca.pem" {
		t.Errorf("TileCAFile.Get() = %q; want the path unchanged", got)
	}
	TileInsecureTLS.SetForTest(t, "1")
	if !TileInsecureTLS.Get() {
		t.Errorf("TileInsecureTLS.Get() = false after being set to 1")
	}
}

// TestClampMaxZoom covers the int64→uint8 mapping: non-positive is "unset"
// (keep the widget default), and over-range saturates instead of wrapping.
func TestClampMaxZoom(t *testing.T) {
	cases := []struct {
		in       int64
		wantZoom uint8
		wantSet  bool
	}{
		{in: 0, wantSet: false},
		{in: -3, wantSet: false},
		{in: 1, wantZoom: 1, wantSet: true},
		{in: 19, wantZoom: 19, wantSet: true},
		{in: 255, wantZoom: 255, wantSet: true},
		{in: 4096, wantZoom: 255, wantSet: true}, // saturates, no uint8 wrap
	}
	for _, tc := range cases {
		zoom, set := clampMaxZoom(tc.in)
		if set != tc.wantSet || (set && zoom != tc.wantZoom) {
			t.Errorf("clampMaxZoom(%d) = (%d, %t); want (%d, %t)",
				tc.in, zoom, set, tc.wantZoom, tc.wantSet)
		}
	}
}

// TestDestinationGatesOnConfiguredURL: the TLS knobs reach the basemap
// destination only once BOXER_MAP_TILE_URL names a server. Both are set here
// and must still come out inert, because the URL in effect is the
// OpenStreetMap default — the case where honouring them would disable
// certificate verification against a public host on the strength of a stray
// environment variable.
func TestDestinationGatesOnConfiguredURL(t *testing.T) {
	TileCAFile.SetForTest(t, "/etc/ssl/gis-ca.pem")
	TileInsecureTLS.SetForTest(t, "1")

	if Configured() {
		t.Fatalf("Configured() = true with BOXER_MAP_TILE_URL unset; the rest of this test is meaningless")
	}
	d, err := ResolveDestination()
	require.NoError(t, err)
	assert.False(t, d.InsecureTLS, "the OSM default must not be downgradable")
	assert.Empty(t, d.CAFile)
	assert.Equal(t, []string{"https://tile.openstreetmap.org/"}, d.Prefixes)

	// Whitespace-only is unset too — the same predicate Configured uses.
	TileURL.SetForTest(t, "   ")
	d, err = ResolveDestination()
	require.NoError(t, err)
	assert.False(t, d.InsecureTLS)
	assert.Empty(t, d.CAFile)

	// A named server: now both knobs land on the destination, and its
	// prefix is the server's.
	TileURL.SetForTest(t, "https://mygis.internal/{z}/{x}/{y}.png")
	d, err = ResolveDestination()
	require.NoError(t, err)
	assert.True(t, d.InsecureTLS)
	assert.Equal(t, "/etc/ssl/gis-ca.pem", d.CAFile)
	assert.Equal(t, []string{"https://mygis.internal/"}, d.Prefixes)
}

// TestDestinationTrimsCAFile pins the trim on the path: a path with a stray
// space is a CA file that silently does not load.
func TestDestinationTrimsCAFile(t *testing.T) {
	TileURL.SetForTest(t, "https://mygis.internal/{z}/{x}/{y}.png")
	TileCAFile.SetForTest(t, "  /etc/ssl/gis-ca.pem\n")
	d, err := ResolveDestination()
	require.NoError(t, err)
	assert.Equal(t, "/etc/ssl/gis-ca.pem", d.CAFile)
}

// An unbound Tiles fails with the reason rather than reaching anywhere.
func TestTilesUnboundFails(t *testing.T) {
	_, err := NewTiles(nil, "test").Get(context.Background(), "https://tile.openstreetmap.org/1/1/1.png")
	require.ErrorIs(t, err, errUnbound)
}

// fetchDirect returns a tile's bytes, refuses a non-200 answer, and refuses
// a body past the loader's own cap.
func TestFetchDirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/1/0/0.png":
			_, _ = w.Write([]byte("png"))
		case "/big.png":
			_, _ = w.Write(make([]byte, maxDirectTileBytes+1))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	data, err := fetchDirect(context.Background(), srv.URL+"/1/0/0.png")
	require.NoError(t, err)
	require.Equal(t, "png", string(data))
	_, err = fetchDirect(context.Background(), srv.URL+"/9/9/9.png")
	require.ErrorContains(t, err, "tile fetch")
	_, err = fetchDirect(context.Background(), srv.URL+"/big.png")
	require.ErrorContains(t, err, "too large")
}

// Outside the browser tab a map shows a basemap by default only when a tile
// server was configured; the tab turns it on (direct_wasip1.go).
func TestDefaultOnFollowsConfiguredOutsideTheTab(t *testing.T) {
	require.Equal(t, Configured(), DefaultOn())
}
