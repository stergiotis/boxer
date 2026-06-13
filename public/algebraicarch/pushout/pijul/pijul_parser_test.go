package pijul

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Pure-parser tests (no `pijul` binary required)
// ---------------------------------------------------------------------------

func TestCreditParserGraphResolution(t *testing.T) {
	mockJSONLog := []byte(`[
	  {
		"hash": "EYPWGEPXCHFDYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYY",
		"authors": ["Alice"],
		"timestamp": "2026-02-22T17:00:00.000000000Z",
		"message": "Added Context Edge"
	  },
	  {
		"hash": "JYS6SYSP25ASXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
		"authors": ["System"],
		"timestamp": "2026-02-22T16:00:00.000000000Z",
		"message": "Init Base Record"
	  }
	]`)

	mockCreditOut := `EYPWGEPXCHFD

EYPWGEPXCHFD, JYS6SYSP25AS
> /id "CUST-100"

JYS6SYSP25AS
> /contact/name "Jane Doe AAAA"

EYPWGEPXCHFD, JYS6SYSP25AS
> /contact/email "jane@example.com"
`

	cells := []KVLine{
		{Path: "/id", Value: "CUST-100"},
		{Path: "/contact/name", Value: "Jane Doe AAAA"},
		{Path: "/contact/email", Value: "jane@example.com"},
	}

	entries, err := ParseLogJSON(mockJSONLog)
	if err != nil {
		t.Fatalf("ParseLogJSON: %v", err)
	}
	processed, cerr := ApplyCreditToCells(mockCreditOut, cells, entries)
	if cerr != nil {
		t.Fatalf("ApplyCreditToCells: %v", cerr)
	}

	for i := range processed {
		if processed[i].Credit == nil {
			t.Errorf("line %d (%s): no Credit attached", i, processed[i].Path)
			continue
		}
		if got := processed[i].Credit.Author(); got != "System" {
			t.Errorf("line %d (%s): expected author %q, got %q",
				i, processed[i].Path, "System", got)
		}
	}
}

func TestParseRecordText_CleanFile(t *testing.T) {
	in := `/id "CUST-100"
/contact/email "jane@example.com"
/account/status "Active"
`
	cells, hasConflict, _ := ParseRecordText(in)
	if hasConflict {
		t.Fatalf("clean file flagged as conflict")
	}
	if len(cells) != 3 {
		t.Fatalf("got %d cells, want 3", len(cells))
	}
	if cells[1].Path != "/contact/email" || cells[1].Value != "jane@example.com" {
		t.Errorf("cell[1]: got %q=%q", cells[1].Path, cells[1].Value)
	}
}

func TestParseRecordText_ConflictBlock(t *testing.T) {
	in := `/id "CUST-100"
>>>>>>> 1
/account/status "Suspended"
=======
/account/status "Pending Approval"
<<<<<<< 2
/contact/email "jane@example.com"
`
	cells, hasConflict, _ := ParseRecordText(in)
	if !hasConflict {
		t.Fatalf("conflict block not detected")
	}
	if len(cells) != 3 {
		t.Fatalf("got %d cells, want 3", len(cells))
	}
	cd := cells[1].Conflict
	if cd == nil {
		t.Fatalf("conflict cell missing Conflict struct")
	}
	if cd.AliceValue != "Suspended" || cd.BobValue != "Pending Approval" {
		t.Errorf("conflict values: alice=%q bob=%q", cd.AliceValue, cd.BobValue)
	}
	if cells[1].Path != "/account/status" {
		t.Errorf("conflict path: got %q", cells[1].Path)
	}
}

func TestParseRecordText_ThreeWayConflict(t *testing.T) {
	in := `/id "CUST-100"
>>>>>>> 1
/account/status "A-side"
=======
/account/status "B-side"
=======
/account/status "C-side"
<<<<<<< 3
`
	cells, hasConflict, _ := ParseRecordText(in)
	if !hasConflict {
		t.Fatalf("3-way conflict not detected")
	}
	if len(cells) != 2 {
		t.Fatalf("got %d cells, want 2", len(cells))
	}
	cd := cells[1].Conflict
	if cd == nil {
		t.Fatalf("conflict cell missing Conflict struct")
	}
	all := cd.AllValues()
	if len(all) != 3 {
		t.Fatalf("AllValues len = %d, want 3 (got %v)", len(all), all)
	}
	if cd.AliceValue != "A-side" || cd.BobValue != "B-side" {
		t.Errorf("first two sides: alice=%q bob=%q", cd.AliceValue, cd.BobValue)
	}
	if len(cd.OtherValues) != 1 || cd.OtherValues[0] != "C-side" {
		t.Errorf("OtherValues = %v, want [C-side]", cd.OtherValues)
	}
}

func TestSerializeRecordText_ThreeWayRoundtrip(t *testing.T) {
	cells := []KVLine{
		{Path: "/id", Value: "CUST-100"},
		{Path: "/foo", Conflict: &ConflictData{
			AliceValue:  "A",
			BobValue:    "B",
			OtherValues: []string{"C", "D"},
		}},
	}
	raw := SerializeRecordText(cells)
	parsed, hasConflict, _ := ParseRecordText(string(raw))
	if !hasConflict {
		t.Fatalf("4-way conflict marker round-trip failed")
	}
	if len(parsed) != 2 {
		t.Fatalf("got %d cells, want 2", len(parsed))
	}
	cd := parsed[1].Conflict
	if cd == nil {
		t.Fatalf("missing conflict on round-trip")
	}
	all := cd.AllValues()
	if len(all) != 4 {
		t.Fatalf("got %d sides, want 4 (%v)", len(all), all)
	}
	for i, want := range []string{"A", "B", "C", "D"} {
		if all[i] != want {
			t.Errorf("side %d: got %q, want %q", i, all[i], want)
		}
	}
}

func TestParseRecordText_EOFNewline(t *testing.T) {
	in := `/id "CUST-100"
/account/status "Active"`
	cells, _, _ := ParseRecordText(in)
	if len(cells) != 2 {
		t.Fatalf("got %d cells, want 2", len(cells))
	}
	if cells[1].Value != "Active" {
		t.Errorf("last cell lost: %q", cells[1].Value)
	}
}

func TestParseRecordText_BlankLinesIgnored(t *testing.T) {
	in := `/id "CUST-100"

/account/status "Active"

`
	cells, _, _ := ParseRecordText(in)
	if len(cells) != 2 {
		t.Fatalf("got %d cells, want 2", len(cells))
	}
}

func TestSerializeRecordText_Roundtrip(t *testing.T) {
	cells := []KVLine{
		{Path: "/id", Value: "CUST-100"},
		{Path: "/foo", Conflict: &ConflictData{AliceValue: "A", BobValue: "B"}},
		{Path: "/baz", Value: "qux"},
	}
	raw := SerializeRecordText(cells)
	parsed, hasConflict, _ := ParseRecordText(string(raw))
	if !hasConflict {
		t.Fatalf("conflict marker round-trip failed")
	}
	if len(parsed) != 3 {
		t.Fatalf("got %d cells, want 3", len(parsed))
	}
	if parsed[1].Conflict == nil || parsed[1].Conflict.AliceValue != "A" || parsed[1].Conflict.BobValue != "B" {
		t.Errorf("conflict round-trip lost values: %+v", parsed[1].Conflict)
	}
	if !strings.HasSuffix(string(raw), "\n") {
		t.Errorf("serializer did not append trailing newline")
	}
}
