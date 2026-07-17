package sockets

import "testing"

// TestParseHexHostPort covers the kernel's little-endian-word hex
// address encoding for both families.
func TestParseHexHostPort(t *testing.T) {
	cases := []struct {
		in       string
		wantAddr string
		wantPort uint16
		wantOk   bool
	}{
		{"0100007F:1F90", "127.0.0.1", 8080, true},
		{"00000000:0016", "0.0.0.0", 22, true},
		{"00000000000000000000000001000000:1F91", "::1", 8081, true},
		{"00000000000000000000000000000000:0035", "::", 53, true},
		// FFFF-mapped v4 as it appears in tcp6 tables.
		{"0000000000000000FFFF00000100007F:0050", "::ffff:127.0.0.1", 80, true},
		{"0100007F", "", 0, false},        // no port separator
		{"0100007F:GGGG", "", 0, false},   // bad port hex
		{"01007F:1F90", "", 0, false},     // odd/short ip hex
		{"XX00007F:1F90", "", 0, false},   // bad ip hex
	}
	for _, tc := range cases {
		addr, port, ok := parseHexHostPort(tc.in)
		if ok != tc.wantOk || addr != tc.wantAddr || port != tc.wantPort {
			t.Errorf("parseHexHostPort(%q) = (%q, %d, %v), want (%q, %d, %v)",
				tc.in, addr, port, ok, tc.wantAddr, tc.wantPort, tc.wantOk)
		}
	}
}
