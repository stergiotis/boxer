//go:build llm_generated_opus47

package progressbar

import (
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0s"},
		{-time.Second, "0s"},
		{5 * time.Second, "5s"},
		{59 * time.Second, "59s"},
		{60 * time.Second, "1m00s"},
		{90 * time.Second, "1m30s"},
		{3599 * time.Second, "59m59s"},
		{3600 * time.Second, "1h00m00s"},
		{3661 * time.Second, "1h01m01s"},
	}
	for _, tc := range cases {
		got := FormatDuration(tc.d)
		if got != tc.want {
			t.Errorf("FormatDuration(%v) = %q; want %q", tc.d, got, tc.want)
		}
	}
}

func TestFormatETA(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{-time.Second, "0s"},
		{0, "0s"},
		{1 * time.Second, "1s"},
		{59 * time.Second, "59s"},
		{60 * time.Second, "1m00s"},
		{599 * time.Second, "9m59s"},
		{600 * time.Second, "~10m"},
		{3599 * time.Second, "~60m"},
		{3600 * time.Second, "~1h"},
		{3900 * time.Second, "~1h05m"},
		{7200 * time.Second, "~2h"},
	}
	for _, tc := range cases {
		got := FormatETA(tc.d)
		if got != tc.want {
			t.Errorf("FormatETA(%v) = %q; want %q", tc.d, got, tc.want)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		b    int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1 << 20, "1.0 MB"},
		{3 << 20, "3.0 MB"},
		{1 << 30, "1.0 GB"},
		{5 << 30, "5.0 GB"},
	}
	for _, tc := range cases {
		got := FormatBytes(tc.b)
		if got != tc.want {
			t.Errorf("FormatBytes(%d) = %q; want %q", tc.b, got, tc.want)
		}
	}
}
