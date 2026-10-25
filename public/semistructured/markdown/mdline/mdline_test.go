package mdline

import "testing"

func TestParsePrefix(t *testing.T) {
	cases := []struct {
		line string
		want Prefix
	}{
		{"plain", Prefix{}},
		{"- item", Prefix{Text: "- ", List: "- "}},
		{"  * item", Prefix{Text: "  * ", List: "* "}},
		{"3. third", Prefix{Text: "3. ", List: "3. ", Ordered: true, Number: 3}},
		{"12) twelve", Prefix{Text: "12) ", List: "12) ", Ordered: true, Number: 12}},
		{"- [x] done", Prefix{Text: "- [x] ", List: "- ", Task: "[x] "}},
		{"> quoted", Prefix{Text: "> ", Quote: true}},
		{"> > deep", Prefix{Text: "> > ", Quote: true}},
		{"> - in quote", Prefix{Text: "> - ", List: "- ", Quote: true}},
		{"-not a list", Prefix{}},
		{"1.no space", Prefix{}},
		{"- ", Prefix{Text: "- ", List: "- "}},
	}
	for _, tc := range cases {
		if got := ParsePrefix(tc.line); got != tc.want {
			t.Errorf("%q: got %+v want %+v", tc.line, got, tc.want)
		}
	}
}

func TestEnter(t *testing.T) {
	if act := Enter("- one\n- two", 11, false); act.Rebind || act.Insert != "\n- " {
		t.Fatalf("continue: %+v", act)
	}
	if act := Enter("1. a\n2. [x] b", 13, false); act.Insert != "\n3. [ ] " {
		t.Fatalf("renumber: %+v", act)
	}
	if act := Enter("> said", 6, false); act.Insert != "\n> " {
		t.Fatalf("quote: %+v", act)
	}
	if act := Enter("  - nested", 10, false); act.Insert != "\n  - " {
		t.Fatalf("nested: %+v", act)
	}
	if act := Enter("- one\n- ", 8, false); !act.Exit || !act.Rebind || act.Next != "- one\n" || act.Caret != 6 {
		t.Fatalf("exit: %+v", act)
	}
	if act := Enter("- one\n- \n- three", 8, false); !act.Exit || act.Next != "- one\n\n- three" {
		t.Fatalf("exit mid-document: %+v", act)
	}
	if act := Enter("- one", 5, true); act.Insert != "\n" || act.Exit {
		t.Fatalf("shift: %+v", act)
	}
	if act := Enter("prose", 5, false); act.Insert != "\n" {
		t.Fatalf("prose: %+v", act)
	}
	if act := Enter("- one", 1, false); act.Insert != "\n" {
		t.Fatalf("inside marker: %+v", act)
	}
	if act := Enter("- one two", 5, false); act.Insert != "\n- " {
		t.Fatalf("split: %+v", act)
	}
	if act := Enter("x", 99, false); act.Insert != "\n" {
		t.Fatalf("clamped: %+v", act)
	}
}

func TestTab(t *testing.T) {
	if act := Tab("- one\n- two", 11, false); !act.Rebind || act.Next != "- one\n\t- two" || act.Caret != 12 {
		t.Fatalf("indent: %+v", act)
	}
	if act := Tab("- one\n\t- two", 12, true); !act.Rebind || act.Next != "- one\n- two" || act.Caret != 11 {
		t.Fatalf("outdent tab: %+v", act)
	}
	if act := Tab("    - two", 9, true); !act.Rebind || act.Next != "- two" || act.Caret != 5 {
		t.Fatalf("outdent spaces: %+v", act)
	}
	if act := Tab("- two", 5, true); act.Rebind || act.Insert != "" {
		t.Fatalf("nothing to outdent: %+v", act)
	}
	if act := Tab("prose", 3, false); act.Insert != "\t" || act.Rebind {
		t.Fatalf("tab outside a list: %+v", act)
	}
	if act := Tab("prose", 3, true); act.Insert != "" || act.Rebind {
		t.Fatalf("shift outside a list: %+v", act)
	}
}

func TestLineBounds(t *testing.T) {
	src := "ab\ncd\nef"
	if LineStart(src, 4) != 3 || LineEnd(src, 4) != 5 || LineStart(src, 0) != 0 || LineEnd(src, 7) != 8 {
		t.Fatal("bounds")
	}
}

func TestCharByteConversions(t *testing.T) {
	s := "aé中b"
	cases := []struct{ ch, by int }{{0, 0}, {1, 1}, {2, 3}, {3, 6}, {4, 7}}
	for _, tc := range cases {
		if got := CharToByte(s, tc.ch); got != tc.by {
			t.Errorf("CharToByte(%d) = %d want %d", tc.ch, got, tc.by)
		}
		if got := ByteToChar(s, tc.by); got != tc.ch {
			t.Errorf("ByteToChar(%d) = %d want %d", tc.by, got, tc.ch)
		}
	}
	if CharToByte(s, 99) != len(s) || ByteToChar(s, 99) != 4 || ByteToChar(s, -1) != 0 {
		t.Error("clamping")
	}
	if ByteToChar(s, 2) != 1 || ByteToChar(s, 4) != 2 {
		t.Error("an offset inside a rune belongs to that rune")
	}
}

func TestWrapSnippet(t *testing.T) {
	src := "some word here"
	if got := WrapSnippet(src, Wraps["bold"], 5, 9); got != "**word**" {
		t.Errorf("selection: %q", got)
	}
	if got := WrapSnippet(src, Wraps["bold"], 9, 5); got != "**word**" {
		t.Errorf("reversed selection: %q", got)
	}
	if got := WrapSnippet(src, Wraps["link"], 3, 3); got != "[label](url)" {
		t.Errorf("placeholder: %q", got)
	}
}

func TestSetHeading(t *testing.T) {
	cases := []struct {
		src   string
		caret int
		level int
		next  string
		at    int
		act   bool
	}{
		{"para", 2, 2, "## para", 5, true},
		{"## para", 5, 0, "para", 2, true},
		{"## para", 5, 3, "### para", 6, true},
		{"## para", 1, 1, "# para", 2, true},
		{"> para", 4, 1, "> # para", 6, true},
		{"- item", 3, 1, "", 0, false},
		{"# same", 3, 1, "", 0, false},
		{"a\n\npara\n\nb", 5, 1, "a\n\n# para\n\nb", 7, true},
		{"#notatag", 3, 1, "# #notatag", 5, true},
	}
	for _, tc := range cases {
		act := SetHeading(tc.src, tc.caret, tc.level)
		if act.Rebind != tc.act {
			t.Errorf("%q level %d: rebind %v", tc.src, tc.level, act.Rebind)
			continue
		}
		if tc.act && (act.Next != tc.next || act.Caret != tc.at) {
			t.Errorf("%q level %d: next %q caret %d", tc.src, tc.level, act.Next, act.Caret)
		}
	}
}
