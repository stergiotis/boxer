package docref

import "testing"

func TestRoundTrip(t *testing.T) {
	cases := []string{
		"help://github.com/stergiotis/boxer/apps/play::snippets#query-graph",
		"help://github.com/stergiotis/boxer/apps/play::howto/replay",
		"adr://0164#sd5",
		"adr://0009",
		"chdoc://quantileTDigest",
	}
	for _, s := range cases {
		ref, err := Parse(s)
		if err != nil {
			t.Errorf("Parse(%q): %v", s, err)
			continue
		}
		if got := ref.String(); got != s {
			t.Errorf("round trip %q -> %q", s, got)
		}
	}
}

func TestFormatMatchesParse(t *testing.T) {
	if got := FormatHelp("a/b/c", "howto/replay", "s"); got != "help://a/b/c::howto/replay#s" {
		t.Errorf("FormatHelp = %q", got)
	}
	if got := FormatAdr(7, ""); got != "adr://0007" {
		t.Errorf("FormatAdr zero-pad = %q", got)
	}
	ref, err := Parse(FormatAdr(164, "sd5"))
	if err != nil || ref.Num != 164 || ref.Section != "sd5" || ref.Scheme != SchemeAdr {
		t.Errorf("adr parse = %+v, %v", ref, err)
	}
}

func TestParseRejects(t *testing.T) {
	for _, s := range []string{
		"",
		"http://example.com",
		"help://no-separator#s",  // missing '::'
		"help://app::",           // empty doc
		"adr://notanumber",
		"chdoc://",
		"chdoc://name#frag", // chdoc has no sections
	} {
		if _, err := Parse(s); err == nil {
			t.Errorf("Parse(%q) should fail", s)
		}
	}
}

func TestZeroRefRendersEmpty(t *testing.T) {
	if got := (Ref{}).String(); got != "" {
		t.Errorf("zero Ref renders %q", got)
	}
}
