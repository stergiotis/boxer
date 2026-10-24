package codeview

import "testing"

func TestBuildStyledSectionsScreensDegenerateLists(t *testing.T) {
	if _, ok := BuildStyledSections(nil); ok {
		t.Fatal("nil list should not build")
	}
	if _, ok := BuildStyledSections([]StyledSection{{Start: 3, Stop: 3, Flags: StyleUnderline}, {Start: 0, Stop: 5}}); ok {
		t.Fatal("empty and flagless sections should not build")
	}
	if _, ok := BuildStyledSections([]StyledSection{{Start: 0, Stop: 5, Flags: StyleFont, Family: FontMonospace, Size: 1}}); !ok {
		t.Fatal("a font-only section is a real section")
	}
}
