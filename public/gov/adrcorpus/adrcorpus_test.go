package adrcorpus

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// writeFile is a tiny test helper that creates parents and writes content.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

// fixture builds a small repo: two ADRs (one fully reviewed, one still carrying
// the commented "# reviewed-by:" template) and one Go file citing both.
func fixture(t *testing.T) (root, adrDir string) {
	t.Helper()
	root = t.TempDir()
	adrDir = filepath.Join(root, "doc", "adr")

	writeFile(t, filepath.Join(adrDir, "0001-sample-decision.md"), `---
type: adr
status: accepted
date: 2026-05-01
reviewed-by: "p@example"
reviewed-date: 2026-05-02
---

# ADR-0001: Sample Decision

## Context

Body text. We build this in Phase 2 after a Cut 1. A horizon of 2099-01-01.

## Update — 2026-05-10

Shipped.
`)

	// Note the commented reviewed-by line: this is the case that used to be
	// mis-parsed as the title.
	writeFile(t, filepath.Join(adrDir, "0002-unreviewed.md"), `---
type: adr
status: proposed
date: 2026-05-03
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD
---

# ADR-0002: Unreviewed Thing

## Context

Nothing references this in code except one place.
`)

	writeFile(t, filepath.Join(root, "code", "foo.go"), `package code
// implements ADR-0001 §SD3 and also touches ADR-0002.
const x = 1
`)
	return root, adrDir
}

func byNum(adrs []Adr, num int) (Adr, bool) {
	for _, a := range adrs {
		if a.Num == num {
			return a, true
		}
	}
	return Adr{}, false
}

func TestParseDir(t *testing.T) {
	_, adrDir := fixture(t)
	adrs, err := ParseDir(adrDir)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	if len(adrs) != 2 {
		t.Fatalf("want 2 ADRs, got %d", len(adrs))
	}

	a1, _ := byNum(adrs, 1)
	if a1.Title != "Sample Decision" {
		t.Errorf("ADR-0001 title: want %q, got %q", "Sample Decision", a1.Title)
	}
	if a1.Status != "accepted" {
		t.Errorf("ADR-0001 status: want accepted, got %q", a1.Status)
	}
	if !a1.HasUpdate || a1.UpdateCount != 1 {
		t.Errorf("ADR-0001 update: want has=true count=1, got has=%v count=%d", a1.HasUpdate, a1.UpdateCount)
	}
	// future-date cutoff: 2099-01-01 must not win over the 2026-05-10 update.
	if a1.LastDate != "2026-05-10" {
		t.Errorf("ADR-0001 last_date: want 2026-05-10 (future 2099 dropped), got %q", a1.LastDate)
	}
	if !slices.Contains(a1.PlanMarkers, "Phase 2") || a1.PlanMaxPhase != 2 {
		t.Errorf("ADR-0001 plan: want Phase 2 / max 2, got %v / %d", a1.PlanMarkers, a1.PlanMaxPhase)
	}

	// Regression: the commented "# reviewed-by:" line must not become the title.
	a2, _ := byNum(adrs, 2)
	if a2.Title != "Unreviewed Thing" {
		t.Errorf("ADR-0002 title: want %q, got %q (frontmatter comment leaked?)", "Unreviewed Thing", a2.Title)
	}
	if a2.ReviewedBy != "" {
		t.Errorf("ADR-0002 reviewed_by: want empty (only commented), got %q", a2.ReviewedBy)
	}
}

// TestScanCodeRefsRelativeRoot is a regression test: the walk root must be
// scanned whatever its basename. A relative root ("..", "../../..") has
// basename "..", which the descendant hidden-directory rule read as a dotfile
// — skipping the entire walk and returning zero citations with no error, so
// every ADR in the corpus would have read as un-built.
func TestScanCodeRefsRelativeRoot(t *testing.T) {
	root, adrDir := fixture(t)
	abs, err := ScanCodeRefs(root, adrDir, "")
	if err != nil {
		t.Fatalf("absolute root: %v", err)
	}
	if len(abs) == 0 {
		t.Fatalf("fixture should yield citations from an absolute root")
	}

	// Walk the same tree via a root whose basename is "..".
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() {
		if cerr := os.Chdir(wd); cerr != nil {
			t.Fatalf("restore cwd: %v", cerr)
		}
	}()
	if err = os.Chdir(filepath.Join(root, "code")); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	rel, err := ScanCodeRefs("..", adrDir, "")
	if err != nil {
		t.Fatalf("relative root: %v", err)
	}
	if len(rel) != len(abs) {
		t.Errorf("relative root %q found %d citations, absolute root found %d — the walk root must not be skipped for its name",
			"..", len(rel), len(abs))
	}
}

func TestScanAndAggregate(t *testing.T) {
	root, adrDir := fixture(t)
	adrs, err := ParseDir(adrDir)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	refs, err := ScanCodeRefs(root, adrDir, "")
	if err != nil {
		t.Fatalf("ScanCodeRefs: %v", err)
	}
	// Two citations on one Go line; the ADR corpus itself is excluded.
	if len(refs) != 2 {
		t.Fatalf("want 2 code refs, got %d: %+v", len(refs), refs)
	}
	var sd3 bool
	for _, r := range refs {
		if r.Num == 1 && r.Qualifier == "SD3" {
			sd3 = true
		}
		if r.Lang != "go" {
			t.Errorf("ref lang: want go, got %q", r.Lang)
		}
	}
	if !sd3 {
		t.Errorf("want ADR-0001 ref with qualifier SD3, got %+v", refs)
	}

	adrs = Aggregate(adrs, refs)
	a1, _ := byNum(adrs, 1)
	if a1.CodeRefs != 1 || a1.ImplEvidence != "referenced" {
		t.Errorf("ADR-0001 evidence: want refs=1 referenced, got refs=%d %q", a1.CodeRefs, a1.ImplEvidence)
	}
	if !slices.Contains(a1.CodeQualifiers, "SD3") {
		t.Errorf("ADR-0001 qualifiers: want SD3, got %v", a1.CodeQualifiers)
	}
}

