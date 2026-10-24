package helphost

import (
	"sync"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
)

// pendingMu guards pendingRefs — the FIFO the host chrome pushes into before
// it opens or raises the help window, so F1 lands on the help of the window
// the person was in. A fresh open pops the head in the factory; a raise
// leaves construction to the past and the live instance drains the queue at
// its next Frame. The capability inspector's status-bar selection has the
// same two halves (capinspector.PushSelection).
var (
	pendingMu   sync.Mutex
	pendingRefs []help.RefT
)

// PushRef enqueues a ref for the next help window to open, or the open one
// to show at its next frame. A zero ref is dropped.
func PushRef(ref help.RefT) {
	if ref.IsZero() {
		return
	}
	pendingMu.Lock()
	defer pendingMu.Unlock()
	pendingRefs = append(pendingRefs, ref)
}

// popRef returns the head of the queue, or the zero ref when it is empty.
func popRef() (ref help.RefT) {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	if len(pendingRefs) == 0 {
		return
	}
	ref = pendingRefs[0]
	pendingRefs = pendingRefs[1:]
	return
}

// RefForApp returns the ref that opens appId's help: its book's overview
// doc when it has one, else the book's first doc in path order. The zero
// ref when the app has no help book, or when appId is the help app itself —
// F1 over the help window leaves it where it is.
func RefForApp(lib help.LibraryI, appId app.AppIdT) (ref help.RefT) {
	if appId == "" || appId == ManifestId {
		return
	}
	b, ok := lib.Book(appId)
	if !ok {
		return
	}
	docs := b.Docs()
	if len(docs) == 0 {
		return
	}
	doc := docs[0].Path
	for _, d := range docs {
		if d.Path == "overview" {
			doc = d.Path
			break
		}
	}
	ref = help.RefT{AppId: appId, Doc: doc}
	return
}
