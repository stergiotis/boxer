package humanfmt_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/observability/humanfmt"
)

func TestBytes(t *testing.T) {
	cases := []struct {
		n    uint64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1 << 10, "1 KiB"},
		{1536, "2 KiB"}, // %.0f rounds 1.5 -> 2
		{1 << 20, "1.0 MiB"},
		{3 * (1 << 20) / 2, "1.5 MiB"},
		{1 << 30, "1.00 GiB"},
		{1 << 40, "1.00 TiB"},
	}
	for _, c := range cases {
		if got := humanfmt.Bytes(c.n); got != c.want {
			t.Errorf("Bytes(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}
