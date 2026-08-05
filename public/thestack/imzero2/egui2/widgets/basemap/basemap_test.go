package basemap

import "testing"

// TestConfigured pins the switch play's Map panel keys its basemap-on default
// on: unset or whitespace-only BOXER_MAP_TILE_URL is "no custom server", a
// non-empty value flips it on.
func TestConfigured(t *testing.T) {
	if Configured() {
		t.Fatalf("Configured() = true with BOXER_MAP_TILE_URL unset")
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

// TestTLSKnobsDefaultToVerifiedPublicRoots pins the safe default: with nothing
// set, neither TLS knob is on, so a basemap fetches under ordinary certificate
// verification. Apply gates both on BOXER_MAP_TILE_URL as well, which this
// cannot observe through the fluid — that gating is asserted by reading Apply,
// and restated renderer-side in build_walkers_tiles.
func TestTLSKnobsDefaultToVerifiedPublicRoots(t *testing.T) {
	if TileInsecureTLS.Get() {
		t.Fatalf("BOXER_MAP_TILE_INSECURE_TLS defaults to true; verification must be on unless asked")
	}
	if TileCAFile.Get() != "" {
		t.Fatalf("BOXER_MAP_TILE_CA_FILE defaults to %q; want empty", TileCAFile.Get())
	}

	// A CA file is a path, not the PEM itself — it is read renderer-side, once
	// per tile-source construction, because the walkersMap opcode ships every
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
