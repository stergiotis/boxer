//go:build llm_generated_opus48

package help

import (
	"testing"
	"testing/fstest"
)

func TestValidateDocInfo_Conformant(t *testing.T) {
	info := DocInfo{Path: "overview", Title: "Overview", Type: "explanation", Status: "stable"}
	if got := ValidateDocInfo(info); len(got) != 0 {
		t.Fatalf("conformant doc: got %d problems, want 0 — %+v", len(got), got)
	}
}

func TestValidateDocInfo_MissingTypeAndStatus(t *testing.T) {
	// No frontmatter at all (Type/Status empty) — both required fields flagged.
	info := DocInfo{Path: "no-frontmatter"}
	got := ValidateDocInfo(info)
	if len(got) != 2 {
		t.Fatalf("got %d problems, want 2 — %+v", len(got), got)
	}
	if got[0].Field != "type" || got[1].Field != "status" {
		t.Errorf("field order: got %q,%q want type,status", got[0].Field, got[1].Field)
	}
	for _, p := range got {
		if p.DocPath != "no-frontmatter" {
			t.Errorf("DocPath not attributed: got %q", p.DocPath)
		}
	}
}

func TestValidateDocInfo_ADRRejectedInHelp(t *testing.T) {
	// ADRs are design history, not operator-facing help: type:adr is a problem
	// even though status:accepted is internally consistent with it.
	info := DocInfo{Path: "misfiled", Type: "adr", Status: "accepted"}
	got := ValidateDocInfo(info)
	if len(got) != 1 {
		t.Fatalf("got %d problems, want 1 — %+v", len(got), got)
	}
	if got[0].Field != "type" || got[0].Value != "adr" {
		t.Errorf("got field=%q value=%q, want type/adr", got[0].Field, got[0].Value)
	}
}

func TestValidateDocInfo_InvalidStatusForType(t *testing.T) {
	// 'accepted' is an ADR-only status; invalid for a descriptive type.
	info := DocInfo{Path: "x", Type: "explanation", Status: "accepted"}
	got := ValidateDocInfo(info)
	if len(got) != 1 || got[0].Field != "status" {
		t.Fatalf("got %+v, want one status problem", got)
	}
}

func TestBook_Validate(t *testing.T) {
	fsys := fstest.MapFS{
		"conformant.md": {Data: []byte("---\ntype: explanation\nstatus: stable\ntitle: OK\n---\n\n# OK\n")},
		"missing.md":    {Data: []byte("# Just a heading\n\nNo frontmatter here.\n")},
		"adr.md":        {Data: []byte("---\ntype: adr\nstatus: accepted\n---\n\n# Misfiled\n")},
	}
	b, err := NewBook("github.com/test/validate", fsys)
	if err != nil {
		t.Fatalf("NewBook: %v", err)
	}
	problems := b.Validate()

	byDoc := map[string]int{}
	for _, p := range problems {
		byDoc[p.DocPath]++
	}
	if byDoc["conformant"] != 0 {
		t.Errorf("conformant doc flagged: %d", byDoc["conformant"])
	}
	if byDoc["missing"] != 2 {
		t.Errorf("missing-frontmatter doc: got %d problems, want 2", byDoc["missing"])
	}
	if byDoc["adr"] != 1 {
		t.Errorf("adr doc: got %d problems, want 1 (type rejected)", byDoc["adr"])
	}

	// Path-sorted: the index is sorted, so problems group by ascending path
	// (adr < missing) and never regress.
	last := ""
	for _, p := range problems {
		if p.DocPath < last {
			t.Errorf("problems not path-sorted: %q after %q", p.DocPath, last)
		}
		last = p.DocPath
	}
}

func TestBook_Validate_AllConformant(t *testing.T) {
	fsys := fstest.MapFS{
		"a.md": {Data: []byte("---\ntype: how-to\nstatus: draft\n---\n\n# A\n")},
		"b.md": {Data: []byte("---\ntype: reference\nstatus: stable\n---\n\n# B\n")},
	}
	b, err := NewBook("github.com/test/clean", fsys)
	if err != nil {
		t.Fatalf("NewBook: %v", err)
	}
	if got := b.Validate(); len(got) != 0 {
		t.Errorf("clean corpus: got %d problems, want 0 — %+v", len(got), got)
	}
}
