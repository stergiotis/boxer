// Fuzzers for the cell-line codec and the record-text parser. The codec
// round-trip is the property that used to fail silently (strings.Trim
// ate quotes); the parser fuzz pins no-panic, error-surfacing, and
// serialize∘parse stability.
package pijul

import (
	"strings"
	"testing"
)

func FuzzCellLineRoundTrip(f *testing.F) {
	f.Add("k", "v")
	f.Add("len", `2"`)
	f.Add("q", `a"b\c"`)
	f.Add("m", "line1\nline2")
	f.Add("e", "")
	f.Fuzz(func(tt *testing.T, path, value string) {
		if validateCellPaths([]KVLine{{Path: path, Value: value}}) != nil {
			tt.Skip("path rejected by validation")
		}
		line := formatCellLine(KVLine{Path: path, Value: value})
		gotPath, gotValue, ok := splitKVLine(strings.TrimSuffix(line, "\n"))
		if !ok {
			tt.Fatalf("formatCellLine output unparseable: %q", line)
		}
		if gotPath != path || gotValue != value {
			tt.Fatalf("round-trip mismatch: (%q,%q) -> %q -> (%q,%q)", path, value, line, gotPath, gotValue)
		}
	})
}

func FuzzParseRecordText(f *testing.F) {
	f.Add("k \"v\"\n")
	f.Add(">>>>>>> 1\nk \"a\"\n=======\nk \"b\"\n<<<<<<< 2\n")
	f.Add(">>>>>>> 1\nk \"a\"\n") // dangling conflict block
	f.Add("")
	f.Fuzz(func(tt *testing.T, content string) {
		cells, _, err := ParseRecordText(content)
		if err != nil {
			return // surfaced scanner errors are fine; silence was the bug
		}
		// Stability: serializing the parsed cells and re-parsing them
		// must reproduce the same cells (serialize is the canonical
		// form). Conflict side labels are not part of the comparison.
		raw := SerializeRecordText(cells)
		again, _, err := ParseRecordText(string(raw))
		if err != nil {
			tt.Fatalf("re-parse of serialized form failed: %v\nserialized: %q", err, raw)
		}
		if len(again) != len(cells) {
			tt.Fatalf("cell count changed across serialize/parse: %d -> %d\nserialized: %q", len(cells), len(again), raw)
		}
		for i := range cells {
			a, b := cells[i], again[i]
			if a.Path != b.Path || a.Value != b.Value {
				tt.Fatalf("cell %d changed: %+v -> %+v", i, a, b)
			}
			if (a.Conflict == nil) != (b.Conflict == nil) {
				tt.Fatalf("cell %d conflictness changed: %+v -> %+v", i, a, b)
			}
			if a.Conflict != nil {
				av, bv := a.Conflict.AllValues(), b.Conflict.AllValues()
				if len(av) != len(bv) {
					tt.Fatalf("cell %d conflict sides changed: %v -> %v", i, av, bv)
				}
				for j := range av {
					if av[j] != bv[j] {
						tt.Fatalf("cell %d side %d changed: %q -> %q", i, j, av[j], bv[j])
					}
				}
			}
		}
	})
}
