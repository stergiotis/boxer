package deploy

import (
	"testing"
	"time"
)

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

func TestSelectPrune(t *testing.T) {
	mk := func(p string, ageSecs int) relEntry {
		return relEntry{path: p, mtime: time.Unix(int64(10000-ageSecs), 0)} // smaller age = newer
	}
	// newest -> oldest: v4, v3, v2, v1, v0
	rels := []relEntry{mk("/r/v2", 2), mk("/r/v4", 0), mk("/r/v0", 4), mk("/r/v3", 1), mk("/r/v1", 3)}

	t.Run("current is newest, keep 2", func(t *testing.T) {
		// keep v3,v2 (newest non-current) + v4 (current); delete v1,v0
		assertSet(t, selectPrune(rels, "/r/v4", 2), []string{"/r/v1", "/r/v0"})
	})
	t.Run("current is older (post-rollback), keep 2", func(t *testing.T) {
		// v1 (current) always kept; keep v4,v3; delete v2,v0
		assertSet(t, selectPrune(rels, "/r/v1", 2), []string{"/r/v2", "/r/v0"})
	})
	t.Run("keep >= count deletes nothing", func(t *testing.T) {
		if got := selectPrune(rels, "/r/v4", 10); len(got) != 0 {
			t.Fatalf("selectPrune keep=10 = %v, want none", got)
		}
	})
}

func assertSet(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	seen := make(map[string]bool, len(got))
	for _, g := range got {
		seen[g] = true
	}
	for _, w := range want {
		if !seen[w] {
			t.Fatalf("got %v, want %v (missing %s)", got, want, w)
		}
	}
}

func TestSupersedes(t *testing.T) {
	cases := []struct {
		name    string
		newest  string // highest release tag found
		current string // running release dir name
		want    bool
	}{
		{"first deploy (no current)", "v1.0.0", "", true},
		{"same tag is not newer", "v1.2.0", "v1.2.0", false},
		{"higher tag supersedes", "v1.2.1", "v1.2.0", true},
		{"lower tag does not", "v1.1.0", "v1.2.0", false},
		{"ref hotfix holds against its base tag", "v0.1.3", "v0.1.3-2-gabc1234", false},
		{"ref hotfix superseded by next release", "v0.1.4", "v0.1.3-2-gabc1234", true},
		{"commit-named ref has zero floor", "v0.1.0", "commit-abc1234", true},
		{"varied component counts", "v1.0.1", "v1.0", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := supersedes(tc.newest, tc.current); got != tc.want {
				t.Fatalf("supersedes(%q, %q) = %v, want %v", tc.newest, tc.current, got, tc.want)
			}
		})
	}
}

func TestFloorVersion(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []int
		ok   bool
	}{
		{"plain tag", "v0.1.3", []int{0, 1, 3}, true},
		{"no v prefix", "1.2", []int{1, 2}, true},
		{"describe name", "v0.1.3-2-gabc1234", []int{0, 1, 3}, true},
		{"commit-named (no ancestor tag)", "commit-abc1234", nil, false},
		{"empty", "", nil, false},
		{"non-release tag base", "nightly-2-gabc1234", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := floorVersion(tc.in)
			if ok != tc.ok || !equalInts(got, tc.want) {
				t.Fatalf("floorVersion(%q) = (%v, %v), want (%v, %v)", tc.in, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
