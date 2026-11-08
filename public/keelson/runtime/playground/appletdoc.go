package playground

import (
	"sync/atomic"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// AppletDoc is an applet document reduced to what a playground window
// applies when it opens a bundle (ADR-0288 §SD4, ADR-0361 §SD5).
type AppletDoc struct {
	Title    string
	Sql      string
	BandsSql string
	// Datasets are the local names the document reads in keelson('…').
	Datasets []string
	// Tab is the first pane the document names, empty for none.
	Tab string
	// Introspection is true when the document asks for the introspection
	// endpoint, where ad-hoc datasets resolve.
	Introspection bool
	Preamble      []byte
	// Runnable is true when the document's SQL is a plain read, which a
	// window runs on open, as an applet's (ADR-0132 §SD5).
	Runnable bool
}

// AppletDocParserFunc parses an applet document.
type AppletDocParserFunc func(path string, src []byte) (doc AppletDoc, err error)

var appletDocParser atomic.Pointer[AppletDocParserFunc]

// SetAppletDocParser installs the applet document parser and returns the one
// it replaced, nil when none was installed. A nil fn clears the slot.
// sqlapplet installs its own at init (ADR-0361 §SD5); a host that links no
// parser refuses bundles with a reason.
func SetAppletDocParser(fn AppletDocParserFunc) (previous AppletDocParserFunc) {
	var next *AppletDocParserFunc
	if fn != nil {
		next = &fn
	}
	if prev := appletDocParser.Swap(next); prev != nil {
		previous = *prev
	}
	return
}

// HasAppletDocParser reports whether a parser is installed.
func HasAppletDocParser() (ok bool) {
	return appletDocParser.Load() != nil
}

// ParseAppletDoc parses src with the installed parser. Without one the
// refusal says why, so it can be shown to the person or the model.
func ParseAppletDoc(path string, src []byte) (doc AppletDoc, err error) {
	fn := appletDocParser.Load()
	if fn == nil {
		return doc, app.RefuseOperation("no applet document parser is linked into this host, so a bundle cannot be opened")
	}
	return (*fn)(path, src)
}
