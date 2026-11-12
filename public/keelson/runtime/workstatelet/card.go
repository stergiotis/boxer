package workstatelet

import (
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// card is what a minted manifest's constructor yields (ADR-0369 §SD3). A
// window host never constructs it: its Open of a workstatelet opens the
// document's windows instead. A host that constructs every manifest it is
// given draws this: what the document is and which windows it lists.
type card struct {
	manifest app.Manifest
	doc      Doc
}

var _ app.AppI = (*card)(nil)

func newCard(m app.Manifest, doc Doc) (inst *card) {
	return &card{manifest: m, doc: doc}
}

func (inst *card) Manifest() (m app.Manifest)                { return inst.manifest }
func (inst *card) Mount(ctx app.MountContextI) (err error)   { return }
func (inst *card) Unmount(ctx app.MountContextI) (err error) { return }

func (inst *card) Frame(ctx app.FrameContextI) (err error) {
	c.Label(inst.doc.Summary).Send()
	if inst.doc.Desktop.Layout != "" {
		c.Label("Desktop layout: " + inst.doc.Desktop.Layout).Send()
	}
	for i, w := range inst.doc.Windows {
		c.Label(strconv.Itoa(i+1) + ". " + WindowLabel(w)).Send()
	}
	return
}

// WindowLabel is one window of a document in a line of text: the app or the
// kind it names, and what it carries.
func WindowLabel(w Window) (s string) {
	s = w.App
	if s == "" {
		s = "the app that takes " + w.Kind
	}
	if len(w.Config) > 0 {
		s += " with a " + w.Kind + " config of " + strconv.Itoa(len(w.Config)) + " bytes"
	}
	if w.Floating {
		s += ", floating"
	}
	return
}
