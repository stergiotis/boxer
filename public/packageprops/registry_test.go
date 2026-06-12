package packageprops

import "testing"

func TestRegistry(t *testing.T) {
	Register("example.com/z", Props{WASMWASI: WASMCompiles})
	Register("example.com/a", Props{WASMWASI: WASMBlocked})
	Register("example.com/a", Props{WASMWASI: WASMCompiles}) // last write wins

	if p, ok := Lookup("example.com/a"); !ok || p.WASMWASI != WASMCompiles {
		t.Errorf("Lookup(a) = %v,%v; want compiles,true", p.WASMWASI, ok)
	}
	if _, ok := Lookup("example.com/absent"); ok {
		t.Error("Lookup of an unregistered path should report ok=false")
	}

	all := All()
	prev := ""
	found := make(map[string]bool, len(all))
	for _, e := range all {
		if prev != "" && e.ImportPath < prev {
			t.Errorf("All() not sorted: %q after %q", e.ImportPath, prev)
		}
		prev = e.ImportPath
		found[e.ImportPath] = true
	}
	if !found["example.com/a"] || !found["example.com/z"] {
		t.Errorf("All() missing registered entries: %v", all)
	}
}
