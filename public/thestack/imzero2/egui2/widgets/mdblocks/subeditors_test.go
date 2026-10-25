package mdblocks

import "testing"

func TestSubKindOf(t *testing.T) {
	if subKindOf("```go\nx\n```", kindCode) != subCode || subKindOf("    indented", kindCode) != subNone {
		t.Fatal("code")
	}
	if subKindOf("| a |\n| - |\n| 1 |", kindTable) != subTable || subKindOf("| a |", kindTable) != subNone {
		t.Fatal("table")
	}
	if subKindOf("> [!note] T\n> body", kindCallout) != subCallout || subKindOf("> plain", kindCallout) != subNone {
		t.Fatal("callout")
	}
	if subKindOf("![alt](u.png)", kindParagraph) != subImage || subKindOf("![[f.png|300]]", kindParagraph) != subImage || subKindOf("text ![alt](u.png)", kindParagraph) != subNone {
		t.Fatal("image")
	}
}

func TestCodeSubRoundTrips(t *testing.T) {
	var s subState
	text := "```go\nfunc main() {}\n\n```"
	s.load(text, subCode, 0)
	if s.codeLang != "go" || s.codeBody != "func main() {}\n" || s.codeClose != "```" {
		t.Fatalf("%+v", s)
	}
	if s.compose() != text {
		t.Fatalf("round trip %q", s.compose())
	}
	s.codeLang = "rust"
	if s.compose() != "```rust\nfunc main() {}\n\n```" {
		t.Fatalf("%q", s.compose())
	}
	// unclosed fence, tilde fence inside a quote, and an empty body
	s.load("> ~~~~\n> x", subCode, 0)
	if s.codeOpenRun != "> ~~~~" || s.codeClose != "" || s.compose() != "> ~~~~\n> x" {
		t.Fatalf("%+v", s)
	}
	s.load("```\n```", subCode, 0)
	if s.codeBody != "" || s.codeClose != "```" || s.compose() != "```\n```" {
		t.Fatalf("empty: %+v %q", s, s.compose())
	}
}

func TestCalloutSubRoundTrips(t *testing.T) {
	var s subState
	text := "> [!warning]- Mind\n> first\n>\n> > nested"
	s.load(text, subCallout, 0)
	if s.calType != "warning" || s.calFold != "-" || s.calTitle != "Mind" || s.calBody != "first\n\n> nested" {
		t.Fatalf("%+v", s)
	}
	if s.compose() != text {
		t.Fatalf("round trip %q", s.compose())
	}
	s.calFold, s.calTitle = "", ""
	if s.compose() != "> [!warning]\n> first\n>\n> > nested" {
		t.Fatalf("%q", s.compose())
	}
}

func TestImageSubRoundTrips(t *testing.T) {
	var s subState
	s.load(`![An alt](pic.png "A title")`, subImage, 0)
	if s.imgAlt != "An alt" || s.imgTarget != "pic.png" || s.imgTit != "A title" || s.compose() != `![An alt](pic.png "A title")` {
		t.Fatalf("%+v %q", s, s.compose())
	}
	s.load("![[Engelbart.jpg|100x145]]", subImage, 0)
	if !s.imgWiki || s.imgTarget != "Engelbart.jpg" || s.imgSize != "100x145" || s.compose() != "![[Engelbart.jpg|100x145]]" {
		t.Fatalf("%+v", s)
	}
	s.imgSize = ""
	if s.compose() != "![[Engelbart.jpg]]" {
		t.Fatal(s.compose())
	}
}

func TestTableSubKeepsUntouchedRowsVerbatim(t *testing.T) {
	var s subState
	text := "| a  |  b |\n|:--|--:|\n| 1 | two \\| pipe |\n|3|4|"
	s.load(text, subTable, 0)
	if len(s.tblRows) != 3 || len(s.tblAlign) != 2 || s.tblRows[1][1] != "two | pipe" || s.tblRows[2][0] != "3" {
		t.Fatalf("%+v", s.tblRows)
	}
	if s.compose() != text {
		t.Fatalf("round trip:\n%q\n%q", s.compose(), text)
	}
	s.tblRows[2][1] = "four"
	want := "| a  |  b |\n|:--|--:|\n| 1 | two \\| pipe |\n| 3 | four |"
	if s.compose() != want {
		t.Fatalf("edited row only:\n%q\n%q", s.compose(), want)
	}
	s.addColumn()
	if s.compose() != "| a | b |  |\n| :-- | --: | --- |\n| 1 | two \\| pipe |  |\n| 3 | four |  |" {
		t.Fatalf("add column:\n%q", s.compose())
	}
}

func TestTableMoveCellAddsARowPastTheEnd(t *testing.T) {
	var s subState
	s.load("| a | b |\n| - | - |\n| 1 | 2 |", subTable, 0)
	s.moveCell(1, 0, true, false)
	if s.tblFocusR != 1 || s.tblFocusC != 1 || len(s.tblRows) != 2 {
		t.Fatalf("tab: %+v", s)
	}
	s.moveCell(1, 1, true, false)
	if s.tblFocusR != 2 || s.tblFocusC != 0 || len(s.tblRows) != 3 {
		t.Fatalf("tab past the end adds a row: %d %d %d", s.tblFocusR, s.tblFocusC, len(s.tblRows))
	}
	s.moveCell(2, 0, true, true)
	if s.tblFocusR != 1 || s.tblFocusC != 1 {
		t.Fatalf("shift+tab: %d %d", s.tblFocusR, s.tblFocusC)
	}
	s.moveCell(0, 1, false, false)
	if s.tblFocusR != 1 || s.tblFocusC != 1 {
		t.Fatalf("enter: %d %d", s.tblFocusR, s.tblFocusC)
	}
}

func TestSubEditorWriteBackSplicesOnlyTheDifference(t *testing.T) {
	src := "intro\n\n```go\nx := 1\n```\n\nafter"
	e := newTestEditor(t, src)
	e.FocusAt(10)
	frame(e)
	if e.sub.kind != subCode || e.sub.codeBody != "x := 1" {
		t.Fatalf("sub not open: %+v", e.sub)
	}
	e.sub.codeBody = "x := 12"
	frame(e)
	if e.src != "intro\n\n```go\nx := 12\n```\n\nafter" {
		t.Fatalf("%q", e.src)
	}
	if n := len(e.hist.undo); n != 1 || e.hist.undo[0].new != "2" || e.hist.undo[0].old != "" {
		t.Fatalf("history: %+v", e.hist.undo)
	}
	if e.sub.kind != subCode || e.sub.codeBody != "x := 12" {
		t.Fatalf("sub lost after its own edit: %+v", e.sub)
	}
}

func TestLeavingASubEditorForAnotherBlockSplicesNothing(t *testing.T) {
	src := "```go\nx\n```\n\npara"
	e := newTestEditor(t, src)
	e.FocusAt(2)
	frame(e)
	if e.sub.kind != subCode {
		t.Fatal("sub not open")
	}
	// a click on the paragraph lands between frames
	e.openBlock(1, 14)
	frame(e)
	if e.src != src || e.sub.kind != subNone || e.buf != "para" {
		t.Fatalf("src %q sub %v buf %q", e.src, e.sub.kind, e.buf)
	}
}

func TestOpeningASubEditorRewritesNothing(t *testing.T) {
	for _, src := range []string{
		"``` go \nx\n```",
		"| a | b |\n| - | - |\n| 1 |",
		">body\n>\n>[!note]",
		"> [!note]\n>body",
		`![a](u  "t") `,
	} {
		e := newTestEditor(t, src)
		e.FocusAt(1)
		frame(e)
		frame(e)
		if e.src != src || len(e.hist.undo) != 0 {
			t.Errorf("%q: opened as %q with %d undo entries", src, e.src, len(e.hist.undo))
		}
	}
}

func TestEditingASubEditorFieldStillSplices(t *testing.T) {
	e := newTestEditor(t, "``` go \nx\n```")
	e.FocusAt(1)
	frame(e)
	e.sub.codeLang = "rust"
	frame(e)
	if e.src != "```rust\nx\n```" {
		t.Fatalf("%q", e.src)
	}
}
