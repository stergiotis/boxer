// Package docref is the canonical string form of a documentation
// reference (ADR-0164 §SD5): the one identifier a search hit carries so
// any surface — a results row, a query result cell, a launch request —
// can navigate to the section it names. Rows written to the facts
// plane outlive whichever process wrote them, which is why the format
// is frozen here, in one leaf package with no dependencies, rather
// than derived ad hoc per producer.
//
// Three schemes, one per corpus:
//
//	help://<app-id>::<doc>#<slug>   a help-book section (help.RefT)
//	adr://<num>#<slug>              an ADR section, num zero-padded to 4
//	chdoc://<name>                  a system.documentation entry
//
// `::` separates the app id from the doc path because app ids are Go
// import paths and contain `/` — a plain slash would make
// `help://a/b/c#s` unparseable. The `#<slug>` fragment is optional
// everywhere it appears; absent means the document (or entry) itself.
package docref

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// SchemeE tags which corpus a parsed ref addresses.
type SchemeE uint8

const (
	// SchemeInvalid is the zero value; Parse never returns it without
	// an error.
	SchemeInvalid SchemeE = iota
	// SchemeHelp — a help-book section: AppId, Doc, Section.
	SchemeHelp
	// SchemeAdr — an ADR section: Num, Section.
	SchemeAdr
	// SchemeChdoc — a system.documentation entry: Name.
	SchemeChdoc
)

// Ref is a parsed documentation reference. Only the fields of the
// active Scheme are populated.
type Ref struct {
	Scheme  SchemeE
	AppId   string // SchemeHelp
	Doc     string // SchemeHelp
	Num     int    // SchemeAdr
	Name    string // SchemeChdoc
	Section string // SchemeHelp, SchemeAdr: heading slug, "" = whole doc
}

// FormatHelp renders a help-book section ref. Empty section refers to
// the document itself.
func FormatHelp(appId string, doc string, section string) (ref string) {
	ref = "help://" + appId + "::" + doc
	if section != "" {
		ref += "#" + section
	}
	return
}

// FormatAdr renders an ADR section ref. The number is zero-padded to
// four digits, matching the corpus filenames.
func FormatAdr(num int, section string) (ref string) {
	ref = fmt.Sprintf("adr://%04d", num)
	if section != "" {
		ref += "#" + section
	}
	return
}

// FormatChdoc renders a system.documentation entry ref.
func FormatChdoc(name string) (ref string) {
	ref = "chdoc://" + name
	return
}

// String re-renders the ref in its canonical form. The zero Ref
// renders empty.
func (inst Ref) String() (s string) {
	switch inst.Scheme {
	case SchemeHelp:
		s = FormatHelp(inst.AppId, inst.Doc, inst.Section)
	case SchemeAdr:
		s = FormatAdr(inst.Num, inst.Section)
	case SchemeChdoc:
		s = FormatChdoc(inst.Name)
	}
	return
}

// Parse reads a canonical ref string. Strict: an unknown scheme, a
// help ref without the `::` separator, or a non-numeric ADR number is
// an error, not a guess — refs live in persisted rows, and a lenient
// parser here would let a malformed producer go unnoticed until its
// rows stop navigating.
func Parse(s string) (ref Ref, err error) {
	switch {
	case strings.HasPrefix(s, "help://"):
		rest, section := splitFragment(s[len("help://"):])
		appId, doc, found := strings.Cut(rest, "::")
		if !found || appId == "" || doc == "" {
			err = eb.Build().Str("ref", s).Errorf("docref: help ref needs '<app-id>::<doc>'")
			return
		}
		ref = Ref{Scheme: SchemeHelp, AppId: appId, Doc: doc, Section: section}
	case strings.HasPrefix(s, "adr://"):
		rest, section := splitFragment(s[len("adr://"):])
		num, convErr := strconv.Atoi(rest)
		if convErr != nil || num < 0 {
			err = eb.Build().Str("ref", s).Errorf("docref: adr ref needs a non-negative number")
			return
		}
		ref = Ref{Scheme: SchemeAdr, Num: num, Section: section}
	case strings.HasPrefix(s, "chdoc://"):
		name := s[len("chdoc://"):]
		if name == "" {
			err = eb.Build().Str("ref", s).Errorf("docref: chdoc ref needs a name")
			return
		}
		if strings.Contains(name, "#") {
			err = eb.Build().Str("ref", s).Errorf("docref: chdoc refs carry no section fragment")
			return
		}
		ref = Ref{Scheme: SchemeChdoc, Name: name}
	default:
		err = eb.Build().Str("ref", s).Errorf("docref: unknown scheme")
	}
	return
}

// splitFragment cuts an optional trailing `#<slug>` off rest. The
// FIRST '#' wins: slugs are lower-cased heading text and never contain
// '#' themselves (markdown.SlugHeading).
func splitFragment(rest string) (body string, section string) {
	body, section, _ = strings.Cut(rest, "#")
	return
}
