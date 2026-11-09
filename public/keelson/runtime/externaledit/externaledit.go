// Package externaledit is the contract by which a window hands a text to an
// editor in another window and gets every saved revision back. The offering
// app gives the text to the service, which holds it as a file in a folder of
// its own and watches it; the app then opens whichever app the host takes
// launchcfg.Kind with, and that editor asks the service where the file is.
// Only the offering window is answered with revisions, and only an editor
// learns where the file is. A revision is what the file holds after a
// completed write, so the offering app is told which session wrote it and
// whether an agent could type there, not that a person did.
//
// The package declares the subjects, the wire types, the client calls and
// the capabilities; the service and the editor are the host's.
package externaledit

import (
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/externaledit/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
)

// The subjects the service answers.
const (
	// SubjectOffer takes a text and answers its id.
	SubjectOffer = "externaledit.offer"
	// SubjectResolve tells an editor where a text's file is.
	SubjectResolve = "externaledit.resolve"
	// SubjectWait answers the offering window the next revision.
	SubjectWait = "externaledit.wait"
	// SubjectClose ends an edit and removes its file.
	SubjectClose = "externaledit.close"
	// SubjectAll covers the four subjects, for the service's declaration.
	SubjectAll = "externaledit.*"
)

// MaxText bounds an offered text and each revision.
const MaxText = 4 << 20

// MaxWait bounds how long one wait request is held.
const MaxWait = 30 * time.Second

// Offer hands a text over.
type Offer struct {
	// Name is the file's name, as the editor shows it; empty is text.txt.
	Name string
	Text []byte
}

// Offered answers an offer.
type Offered struct {
	Id     string
	Reason string
}

// Resolve asks where a text's file is, saying which session will edit it.
type Resolve struct {
	Id string
	// Session names the editing session, as its record does.
	Session string
	// Agent says an agent could type in that session.
	Agent bool
}

// Resolved says where the file is.
type Resolved struct {
	// Dir is the edit's folder on the host, File the file in it.
	Dir    string
	File   string
	Reason string
}

// Wait asks for the first revision after After.
type Wait struct {
	Id    string
	After uint64
	// WaitMs is how long the service may hold the request; at most MaxWait.
	WaitMs uint32
}

// Revision is a saved text, or the end of the edit.
type Revision struct {
	// Seq numbers the revisions from 1; 0 is the offered text.
	Seq    uint64
	Text   []byte
	Digest string
	// Session wrote it; Agent says an agent could type there.
	Session string
	Agent   bool
	// Closed says the edit has ended.
	Closed bool
	Reason string
}

// CloseRequest ends an edit.
type CloseRequest struct {
	Id string
}

// Closed answers a close.
type Closed struct {
	Reason string
}

// ClientCaps are what an app that hands texts over declares: the three
// subjects it calls and the window open that shows the editor.
func ClientCaps(reason string) []app.SubjectFilter {
	return []app.SubjectFilter{
		{Pattern: SubjectOffer, Direction: app.CapDirectionPub, Reason: reason},
		{Pattern: SubjectWait, Direction: app.CapDirectionPub, Reason: reason},
		{Pattern: SubjectClose, Direction: app.CapDirectionPub, Reason: reason},
		{Pattern: windowhost.OpenSubject, Direction: app.CapDirectionPub, Reason: reason},
	}
}

// EditorCaps are what an app that edits handed-over texts declares.
func EditorCaps(reason string) []app.SubjectFilter {
	return []app.SubjectFilter{{Pattern: SubjectResolve, Direction: app.CapDirectionPub, Reason: reason}}
}

// Editor is the display name of the app this host opens handed-over texts
// in, and whether there is exactly one; an app offers the gesture only when
// there is.
func Editor() (display string, ok bool) {
	if id, designated := app.DefaultRegistry.Designation(launchcfg.Kind); designated {
		m, found := app.LookupManifest(id)
		return m.Display, found
	}
	n := 0
	for _, m := range app.DefaultRegistry.LaunchableManifests() {
		if m.LaunchKind == launchcfg.Kind {
			display = m.Display
			n++
		}
	}
	return display, n == 1
}

// IsEditor says whether id is an app that takes handed-over texts.
func IsEditor(id app.AppIdT) bool {
	m, ok := app.LookupManifest(id)
	return ok && m.LaunchKind == launchcfg.Kind
}
