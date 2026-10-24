package mdedit

import "testing"

func TestParseLinePrefix(t *testing.T) {
	cases := []struct {
		line       string
		text, list string
		ordered    bool
		number     int
		task       string
		quote      bool
	}{
		{"plain", "", "", false, 0, "", false},
		{"- item", "- ", "- ", false, 0, "", false},
		{"  * item", "  * ", "* ", false, 0, "", false},
		{"3. third", "3. ", "3. ", true, 3, "", false},
		{"12) twelve", "12) ", "12) ", true, 12, "", false},
		{"- [x] done", "- [x] ", "- ", false, 0, "[x] ", false},
		{"> quoted", "> ", "", false, 0, "", true},
		{"> > deep", "> > ", "", false, 0, "", true},
		{"> - in quote", "> - ", "- ", false, 0, "", true},
		{"-not a list", "", "", false, 0, "", false},
		{"1.no space", "", "", false, 0, "", false},
	}
	for _, tc := range cases {
		p := parseLinePrefix(tc.line)
		if p.text != tc.text || p.list != tc.list || p.ordered != tc.ordered || p.number != tc.number || p.task != tc.task || p.quote != tc.quote {
			t.Errorf("%q: got %+v", tc.line, p)
		}
	}
}

func TestEnterContinuesAList(t *testing.T) {
	src := "- one\n- two"
	act := enterAction(src, len(src), false)
	if act.exit || act.insert != "\n- " {
		t.Fatalf("got %+v", act)
	}
}

func TestEnterRenumbersAnOrderedListAndEmptiesATask(t *testing.T) {
	src := "1. a\n2. [x] b"
	act := enterAction(src, len(src), false)
	if act.insert != "\n3. [ ] " {
		t.Fatalf("got %q", act.insert)
	}
}

func TestEnterContinuesAQuoteAndKeepsIndentation(t *testing.T) {
	if act := enterAction("> said", 6, false); act.insert != "\n> " {
		t.Fatalf("quote: %+v", act)
	}
	if act := enterAction("  - nested", 10, false); act.insert != "\n  - " {
		t.Fatalf("nested: %+v", act)
	}
}

func TestEnterOnAnEmptyItemExitsTheList(t *testing.T) {
	src := "- one\n- "
	act := enterAction(src, len(src), false)
	if !act.exit || act.next != "- one\n" || act.caret != len("- one\n") {
		t.Fatalf("got %+v", act)
	}
	// with text after the caret on the same line it is still exit material
	// only when the line is blank past its marker
	src = "- one\n- \n- three"
	act = enterAction(src, len("- one\n- "), false)
	if !act.exit || act.next != "- one\n\n- three" {
		t.Fatalf("mid-document: %+v", act)
	}
}

func TestEnterIsPlainWhenShiftedOrOutsideAList(t *testing.T) {
	if act := enterAction("- one", 5, true); act.insert != "\n" || act.exit {
		t.Fatalf("shift: %+v", act)
	}
	if act := enterAction("prose", 5, false); act.insert != "\n" {
		t.Fatalf("prose: %+v", act)
	}
	// inside the marker itself: a plain newline
	if act := enterAction("- one", 1, false); act.insert != "\n" {
		t.Fatalf("inside marker: %+v", act)
	}
}

func TestEnterSplitsAnItemInTheMiddle(t *testing.T) {
	src := "- one two"
	act := enterAction(src, len("- one"), false)
	if act.insert != "\n- " {
		t.Fatalf("got %+v", act)
	}
}

func TestTabIndentsAndOutdentsListLines(t *testing.T) {
	src := "- one\n- two"
	caret := len(src)
	act := tabAction(src, caret, false)
	if !act.rebind || act.next != "- one\n\t- two" || act.caret != caret+1 {
		t.Fatalf("indent: %+v", act)
	}
	src = "- one\n\t- two"
	caret = len(src)
	act = tabAction(src, caret, true)
	if !act.rebind || act.next != "- one\n- two" || act.caret != caret-1 {
		t.Fatalf("outdent tab: %+v", act)
	}
	src = "    - two"
	act = tabAction(src, len(src), true)
	if !act.rebind || act.next != "- two" || act.caret != len("- two") {
		t.Fatalf("outdent spaces: %+v", act)
	}
	if act := tabAction("- two", 5, true); act.rebind || act.insert != "" {
		t.Fatalf("nothing to outdent: %+v", act)
	}
}

func TestTabOutsideAListInsertsATab(t *testing.T) {
	if act := tabAction("prose", 3, false); act.insert != "\t" || act.rebind {
		t.Fatalf("got %+v", act)
	}
	if act := tabAction("prose", 3, true); act.insert != "" || act.rebind {
		t.Fatalf("shift outside a list does nothing: %+v", act)
	}
}
