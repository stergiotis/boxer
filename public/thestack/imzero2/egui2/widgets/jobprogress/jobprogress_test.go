package jobprogress

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStatusLine(t *testing.T) {
	cases := []struct {
		name string
		in   Input
		want string
	}{
		{"percent only", Input{Fraction: 0.47}, "47%"},
		{"all parts", Input{Fraction: 0.47, Rate: 1_200, RateUnit: "rows", EtaMs: 125_000, Note: "scanning"}, "47% · 1.2 k rows/s · 2m05s left · scanning"},
		{"amount replaces percent", Input{Fraction: 0.5, Amount: "1.2 M / 2.4 M rows", Rate: 3e5, RateUnit: "rows"}, "1.2 M / 2.4 M rows · 300 k rows/s"},
		{"indeterminate keeps rate, drops eta", Input{Fraction: -1, Rate: 18 * 1024 * 1024, RateUnit: "bytes", EtaMs: 9_000}, "18 MiB/s"},
		{"indeterminate note", Input{Fraction: -1, Note: "extracting…"}, "extracting…"},
		{"sub-second eta", Input{Fraction: 0.99, EtaMs: 300}, "99% · <1s left"},
		{"unknown eta and rate", Input{Fraction: 0.1, EtaMs: -1}, "10%"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, StatusLine(c.in))
		})
	}
}
