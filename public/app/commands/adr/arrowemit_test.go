package adr

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stergiotis/boxer/public/gov/adrcorpus"
)

// fixture builds a one-ADR corpus with a sub-item of each done-ness plus one
// code citation, so every emitted table has at least one row.
func fixture(t *testing.T) (root, adrDir string) {
	t.Helper()
	root = t.TempDir()
	adrDir = filepath.Join(root, "doc", "adr")
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %q: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %q: %v", path, err)
		}
	}
	write(filepath.Join(adrDir, "0001-sample-decision.md"), `---
type: adr
status: accepted
date: 2026-05-01
---

# ADR-0001: Sample Decision

## Decision

- **SD1 — Done one.** ✓ Prose.
- **SD2 — Not done.**
`)
	write(filepath.Join(root, "code", "foo.go"), "package code\n// implements ADR-0001 §SD1.\nconst x = 1\n")
	return root, adrDir
}

// TestEmitArrow checks all three Arrow files are written non-empty from a
// parsed corpus.
func TestEmitArrow(t *testing.T) {
	root, adrDir := fixture(t)
	adrs, err := adrcorpus.ParseDir(adrDir)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	refs, err := adrcorpus.ScanCodeRefs(root, adrDir, "")
	if err != nil {
		t.Fatalf("ScanCodeRefs: %v", err)
	}
	adrs = adrcorpus.Aggregate(adrs, refs)
	subs := adrcorpus.AllSubtasks(adrs)
	if len(subs) != 2 {
		t.Fatalf("want 2 sub-items in the fixture, got %d", len(subs))
	}

	out := t.TempDir()
	for _, tc := range []struct {
		name  string
		path  string
		write func(string) error
	}{
		{adrArrowName, filepath.Join(out, adrArrowName), func(p string) error { return WriteAdrArrow(p, adrs) }},
		{coderefArrowName, filepath.Join(out, coderefArrowName), func(p string) error { return WriteCoderefArrow(p, refs) }},
		{subtaskArrowName, filepath.Join(out, subtaskArrowName), func(p string) error { return WriteSubtaskArrow(p, subs) }},
	} {
		if err := tc.write(tc.path); err != nil {
			t.Fatalf("write %s: %v", tc.name, err)
		}
		fi, err := os.Stat(tc.path)
		if err != nil || fi.Size() == 0 {
			t.Errorf("%s not written or empty: %v", tc.name, err)
		}
	}
}
