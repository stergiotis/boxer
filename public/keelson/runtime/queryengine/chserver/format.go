package chserver

import (
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// sameFormat reports whether two format names, as a request and a response
// header spell them, name one format. ClickHouse matches format names without
// regard to case and echoes the spelling it was given, and the TSV family has
// a second spelling for every member.
func sameFormat(a string, b string) bool {
	return canonicalFormat(a) == canonicalFormat(b)
}

func canonicalFormat(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	if rest, ok := strings.CutPrefix(n, "tsv"); ok {
		n = "tabseparated" + rest
	}
	return n
}

// formatMismatch is the refusal for a body in a format the caller did not ask
// for. It names the likely cause, since a reader has no other way to learn it:
// the server predates X-ClickHouse-Format overriding a FORMAT clause, and the
// statement carries one.
func formatMismatch(got string, want string) (err error) {
	bld := eb.Build().Str("format", got).Str("requested", want)
	err = bld.Errorf("chserver: the server answered in %s, not the requested %s: the statement carries its own FORMAT clause, which servers before ClickHouse 26.8 let win over X-ClickHouse-Format", got, want) //boxer:lint disable=CS013 reason="the GUI renders err.Error(); the two format names are what the reader acts on"
	return
}
