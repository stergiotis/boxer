package sqlapplet

import (
	"sync"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appletstore"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// storeBookID is the book id a stored definition carries, which the
// definition drawer and the logs show as its provenance.
const storeBookID = "store"

// StoreMinter is sqlapplet's side of the runtime applet store
// (appletstore.MinterI, ADR-0132 Update "O4"): the parser the committed
// books go through, and the manifest and window of each applet the store
// admits. It keeps the live definition of every stored applet it minted,
// which the minted factory resolves at each open, so an overwrite reaches
// future windows without re-registration (O4-D4). The gate itself, which
// documents get here, is the store's.
type StoreMinter struct {
	mu   sync.Mutex
	defs map[string]*AppletDef // stored applets only; resolved at Open time
}

var _ appletstore.MinterI = (*StoreMinter)(nil)

// NewStoreMinter returns the minter the wiring site hands to
// appletstore.Start.
func NewStoreMinter() (inst *StoreMinter) {
	inst = &StoreMinter{defs: make(map[string]*AppletDef, 4)}
	return
}

// Parse implements appletstore.MinterI with [ParseDocSource], the parser the
// committed corpus gate runs.
func (inst *StoreMinter) Parse(slug string, doc []byte) (def appletstore.Definition, applet bool, err error) {
	d, err := ParseDocSource(storeBookID, slug+".md", doc)
	if err != nil || d == nil {
		return
	}
	def = storeDefinition(d, doc)
	applet = true
	return
}

// storeDefinition is the part of d the store's gate reads.
func storeDefinition(d *AppletDef, doc []byte) (def appletstore.Definition) {
	def = appletstore.Definition{
		Slug:  d.Slug,
		AppId: app.AppIdT(appletIdPrefix + d.Slug),
		Class: d.Class,
		Doc:   doc,
	}
	return
}

// reparse turns an admitted definition back into the applet definition; the
// store hands over the document it persisted, which Parse already accepted.
func (inst *StoreMinter) reparse(def appletstore.Definition) (d *AppletDef, err error) {
	d, err = ParseDocSource(storeBookID, def.Slug+".md", def.Doc)
	if err == nil && d == nil {
		err = eb.Build().Str("slug", def.Slug).Errorf("sqlapplet: the stored document carries no sql fence")
	}
	return
}

func (inst *StoreMinter) lookup(slug string) (def *AppletDef) {
	inst.mu.Lock()
	def = inst.defs[slug]
	inst.mu.Unlock()
	return
}

// Mint implements appletstore.MinterI: it registers the manifest of a stored
// definition, whose factory resolves the live definition at every open.
func (inst *StoreMinter) Mint(reg *app.Registry, def appletstore.Definition) (err error) {
	d, err := inst.reparse(def)
	if err != nil {
		return
	}
	// ADR-0158 §SD7: a stored applet inherits no book, so it falls back to
	// the store default unless its document classified itself.
	if len(d.Topics) == 0 {
		d.Topics = storeDefaultTopics
	}
	m := manifestFor(d, nil)
	if err = m.Validate(); err != nil {
		return
	}
	slug := d.Slug
	if err = reg.RegisterFactory(m, func() (a app.AppI, ctorErr error) {
		live := inst.lookup(slug)
		if live == nil {
			ctorErr = eb.Build().Str("slug", slug).Errorf("sqlapplet: stored applet has no live definition")
			return
		}
		a = &appletApp{def: live, m: m}
		return
	}); err != nil {
		return
	}
	inst.mu.Lock()
	inst.defs[slug] = d
	inst.mu.Unlock()
	return
}

// Replace implements appletstore.MinterI: the live definition swaps, and the
// minted manifest's display metadata stays as registered until the next
// boot (O4-D4).
func (inst *StoreMinter) Replace(def appletstore.Definition) (err error) {
	d, err := inst.reparse(def)
	if err != nil {
		return
	}
	inst.mu.Lock()
	inst.defs[d.Slug] = d
	inst.mu.Unlock()
	return
}
