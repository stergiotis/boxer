package ignoreann

import (
	"go/parser"
	"go/token"
	"testing"
)

// posAtLine returns the token.Pos of the first character on the given 1-based
// line of file. Used so test assertions can refer to lines symbolically.
func posAtLine(t *testing.T, fset *token.FileSet, file *token.File, line int) (p token.Pos) {
	t.Helper()
	if line < 1 || line > file.LineCount() {
		t.Fatalf("line %d out of range [1, %d]", line, file.LineCount())
	}
	p = file.LineStart(line)
	return
}

func TestSuppressedTrailingComment(t *testing.T) {
	const src = `package p

func _() {
	_ = 1 // designlint:ignore=L2 (test reason)
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "p.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	idx := Build(fset, file)
	tokFile := fset.File(file.Pos())

	if !idx.Suppressed(posAtLine(t, fset, tokFile, 4), "L2") {
		t.Errorf("trailing comment on line 4: want suppressed for L2")
	}
	if idx.Suppressed(posAtLine(t, fset, tokFile, 4), "L5") {
		t.Errorf("trailing comment on line 4: should not suppress L5")
	}
}

func TestSuppressedPrecedingLine(t *testing.T) {
	const src = `package p

func _() {
	// designlint:ignore=L9 (test reason)
	_ = 1
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "p.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	idx := Build(fset, file)
	tokFile := fset.File(file.Pos())
	// Comment on line 4 covers lines 4 and 5.
	if !idx.Suppressed(posAtLine(t, fset, tokFile, 5), "L9") {
		t.Errorf("preceding-line comment on line 4: want suppressed for L9 at line 5")
	}
}

func TestSuppressedMultipleRules(t *testing.T) {
	const src = `package p

// designlint:ignore=L2,L5,L9 (intentional)
var _ = 0
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "p.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	idx := Build(fset, file)
	tokFile := fset.File(file.Pos())
	for _, id := range []string{"L2", "L5", "L9"} {
		if !idx.Suppressed(posAtLine(t, fset, tokFile, 4), id) {
			t.Errorf("comma-separated rule list: want suppressed for %s at line 4", id)
		}
	}
	if idx.Suppressed(posAtLine(t, fset, tokFile, 4), "L1") {
		t.Errorf("comma-separated rule list: should not suppress unlisted L1")
	}
}

func TestNilIndexNotSuppressed(t *testing.T) {
	var idx *Index
	if idx.Suppressed(token.NoPos, "L2") {
		t.Errorf("nil index should never suppress")
	}
}
