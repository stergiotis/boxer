package deploy

import "testing"

func TestSelectNewestTag(t *testing.T) {
	cases := []struct {
		name string
		tags []string
		want string
		ok   bool
	}{
		{"empty", nil, "", false},
		{"no release tags", []string{"nightly", "latest", "foo-bar"}, "", false},
		{"simple", []string{"v1.0.0", "v1.2.0", "v1.1.9"}, "v1.2.0", true},
		{"numeric not lexical", []string{"v1.9.0", "v1.10.0", "v1.2.0"}, "v1.10.0", true},
		{"mixed prefix and plain", []string{"1.0.0", "v2.0.0", "junk"}, "v2.0.0", true},
		{"varied component count", []string{"v1", "v1.0", "v1.0.1"}, "v1.0.1", true},
		{"non-release noise ignored", []string{"release-candidate", "v3.1.4", "v3.1.4-rc1"}, "v3.1.4", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := selectNewestTag(tc.tags)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("selectNewestTag(%v) = (%q, %v), want (%q, %v)", tc.tags, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestParseProbe(t *testing.T) {
	const out = "hello #1: 1280x800 @ppp 1\nprobe done: 42 AUs, 123456 bytes, 1 keyframes -> /tmp/x.h264\n"
	aus, kf := parseProbe(out)
	if aus != 42 || kf != 1 {
		t.Fatalf("parseProbe = (%d, %d), want (42, 1)", aus, kf)
	}
	if a, k := parseProbe("nonsense"); a != 0 || k != 0 {
		t.Fatalf("parseProbe(nonsense) = (%d, %d), want (0, 0)", a, k)
	}
}
