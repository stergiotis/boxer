package basemap

import "testing"

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

// TestPortolanLoaderGatesOnConfiguredURL is the assertion the comment on
// TestTLSKnobsDefaultToVerifiedPublicRoots used to promise and never made: the
// TLS knobs reach the loader only once BOXER_MAP_TILE_URL names a server. Both
// are set here and must still come out inert, because the URL in effect is the
// OpenStreetMap default — the case where honouring them would disable
// certificate verification against a public host on the strength of a stray
// environment variable.
func TestPortolanLoaderGatesOnConfiguredURL(t *testing.T) {
	TileCAFile.SetForTest(t, "/etc/ssl/gis-ca.pem")
	TileInsecureTLS.SetForTest(t, "1")

	// BOXER_MAP_TILE_URL unset: the default OSM server is in effect, so
	// neither knob applies however loudly the environment asks.
	if Configured() {
		t.Fatalf("Configured() = true with BOXER_MAP_TILE_URL unset; the rest of this test is meaningless")
	}
	opts := PortolanLoader()
	if opts.InsecureTLS {
		t.Errorf("PortolanLoader().InsecureTLS = true with BOXER_MAP_TILE_URL unset; the OSM default must not be downgradable")
	}
	if opts.CAFile != "" {
		t.Errorf("PortolanLoader().CAFile = %q with BOXER_MAP_TILE_URL unset; want empty", opts.CAFile)
	}

	// Whitespace-only is unset too — the same predicate Configured uses.
	TileURL.SetForTest(t, "   ")
	if opts := PortolanLoader(); opts.InsecureTLS || opts.CAFile != "" {
		t.Errorf("PortolanLoader() = %+v with whitespace-only BOXER_MAP_TILE_URL; want the zero options", opts)
	}

	// A named server: now both knobs land on the loader options.
	TileURL.SetForTest(t, "https://mygis.internal/{z}/{x}/{y}.png")
	opts = PortolanLoader()
	if !opts.InsecureTLS {
		t.Errorf("PortolanLoader().InsecureTLS = false with BOXER_MAP_TILE_URL set; the knob never reaches the loader")
	}
	if opts.CAFile != "/etc/ssl/gis-ca.pem" {
		t.Errorf("PortolanLoader().CAFile = %q; want /etc/ssl/gis-ca.pem", opts.CAFile)
	}
}

// TestPortolanLoaderTrimsCAFile pins the trim on the path: an env var edited
// by hand or through a YAML block picks up trailing whitespace easily, and a
// path with a stray space is a CA file that silently does not load.
func TestPortolanLoaderTrimsCAFile(t *testing.T) {
	TileURL.SetForTest(t, "https://mygis.internal/{z}/{x}/{y}.png")
	TileCAFile.SetForTest(t, "  /etc/ssl/gis-ca.pem\n")
	if got := PortolanLoader().CAFile; got != "/etc/ssl/gis-ca.pem" {
		t.Errorf("PortolanLoader().CAFile = %q; want the trimmed path", got)
	}
}
