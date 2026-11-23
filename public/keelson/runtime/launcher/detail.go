package launcher

// The detail pane (ADR-0214 §SD6). It renders what the manifest already
// declares — the eighteen columns keelson('apps') has always serialised and
// no launcher surface could read — for whichever row is selected.
//
// Deliberately not the app's rendered help document. §SD6 named the help
// book's lead doc as detail-pane content, and embedding markdown here was
// descoped rather than gated: markdown.Doc.Render's ids come from a
// per-Render sequence whose generation the caller has to key an IdScope on,
// and a launcher that has to be rev'd when the markdown renderer learns a
// segment kind is a bad trade for prose the Help center is one click away
// from. What lands instead is the book's contents list plus that one click.

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// renderDetailPane draws the selected app, or says what to do when nothing is
// selected. The empty state offers a next step rather than explaining the
// mechanism (§SD10's last anti-pattern).
func (inst *Inst) renderDetailPane(ids *c.WidgetIdStack) {
	if inst.selected == "" {
		c.Label("Pick an app on the left to see what it does.").Send()
		return
	}
	m, ok := inst.registry.LookupManifest(inst.selected)
	if !ok {
		// A selection can outlive its manifest: applets are minted at boot
		// and a store overwrite replaces one. Clearing rather than reporting
		// keeps a transient registry edit from looking like a failure.
		inst.selected = ""
		return
	}
	for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
		inst.renderDetailHead(ids, m)
		inst.renderDetailActions(ids, m)
		c.Separator().Horizontal().Send()
		inst.renderDetailMeta(m)
		inst.renderDetailCaps(m)
		inst.renderDetailHelp(ids, m)
		inst.renderDetailMore(ids, m)
	}
}

// renderDetailHead draws the identity block: icon, name, and the summary as
// the pane's own lead line.
func (inst *Inst) renderDetailHead(ids *c.WidgetIdStack, m app.Manifest) {
	for range c.Horizontal().KeepIter() {
		if m.Icon != "" {
			c.Label(m.Icon).Send()
		}
		for rt := range c.RichTextLabel(rowLabel(m)) {
			rt.Strong()
		}
		sendKindBadge(ids, "detail-kind", m.Kind)
		sendMaturityBadge(ids, "detail-maturity", m.Maturity)
	}
	if m.Summary != "" {
		c.Label(m.Summary).Send()
	}
	// Title only when it says more than Display does — an app whose window
	// title is a longer form of its menu label ("mdedit" / "mdedit — markdown
	// editor") is worth showing once, and one where they match is not.
	if t := m.WindowTitle(); t != "" && !strings.EqualFold(strings.TrimSpace(t), strings.TrimSpace(m.Display)) {
		for rt := range c.RichTextLabel("Window title: " + t) {
			rt.Small().Weak()
		}
	}
	c.AddSpace(styletokens.GapItems(inst.density))
}

// renderDetailActions draws the verbs (§SD5's "a row is a noun with several
// verbs"). Open is the default one the row already performs; the others exist
// because they were previously unreachable from any launcher surface. New
// window appears only while a window exists, since Open already opens one
// otherwise.
//
// Open is the pane's primary button (ADR-0273). While the row list holds
// focus, Space and Enter perform it (keys.go), and a weak hint at the end of
// the row says so: it is what makes the keyboard contract visible to someone
// who arrived by clicking a row rather than by reading an ADR. The hint
// replaces drawing Open selected, which swapped the primary fill for the
// selection colours exactly while the button was armed.
func (inst *Inst) renderDetailActions(ids *c.WidgetIdStack, m app.Manifest) {
	_, isOpen := inst.openAppSet()[m.Id]
	// The primary button is the launch action Enter and a double click run;
	// a factory app with a window also offers Raise, so reaching the window
	// already open stays one click (ADR-0214 §Updates 2026-11-12).
	verb := inst.launchVerb(m.Id, isOpen)
	label := icons.PhArrowSquareOut + " Open"
	hint := "Enter opens"
	switch verb {
	case launchVerbRaise:
		label = icons.PhArrowSquareOut + " Raise"
		hint = "Enter raises"
	case launchVerbNewWindow:
		label = icons.PhPlus + " New window"
		hint = "Enter opens a new window"
	}
	for range c.Horizontal().KeepIter() {
		if c.Button(ids.PrepareStr("detail-open"), c.Atoms().Text(label).Keep()).
			Kind(c.ButtonKindPrimary).
			SendResp().HasPrimaryClicked() {
			inst.launch(m.Id)
		}
		if verb == launchVerbNewWindow {
			if c.Button(ids.PrepareStr("detail-raise"), c.Atoms().Text(icons.PhArrowSquareOut+" Raise").Keep()).
				SendResp().HasPrimaryClicked() {
				inst.open(m.Id)
			}
		}
		if inst.helpAppId != "" && inst.hasHelp(m) && inst.registry.Launchable(inst.helpAppId) {
			if c.Button(ids.PrepareStr("detail-help"), c.Atoms().Text(icons.PhBookOpen+" Help").Keep()).
				SendResp().HasPrimaryClicked() {
				inst.openHelp(overviewDoc(inst.lib, m.Id), m.Id)
			}
		}
		if inst.inspect != nil {
			if c.Button(ids.PrepareStr("detail-inspect"), c.Atoms().Text(icons.PhMagnifyingGlass+" Inspect").Keep()).
				SendResp().HasPrimaryClicked() {
				inst.inspectApp(m.Id)
			}
		}
		if inst.listHasFocus() {
			for rt := range c.RichTextLabel(hint) {
				rt.Small().Weak()
			}
		}
	}
	c.AddSpace(styletokens.GapItems(inst.density))
}

// renderDetailMeta draws the key-value block a person choosing an app reads:
// the topics it is listed under and whether it remembers its state.
//
// Rows appear only when they hold something. The builder's columns — the
// search keywords, the version, the module, the launch kind, the stored keys
// — are in renderDetailMore's fold, one click away, rather than between the
// summary and what the app touches.
func (inst *Inst) renderDetailMeta(m app.Manifest) {
	rows := make([][2]string, 0, 2)
	if len(m.Topics) > 0 {
		labels := make([]string, 0, len(m.Topics))
		for _, t := range m.Topics {
			labels = append(labels, topicLabel(t))
		}
		rows = append(rows, [2]string{"Topics", strings.Join(labels, " · ")})
	}
	if m.Workingset {
		rows = append(rows, [2]string{"Remembers", "reopens with the state you left"})
	}
	renderKeyValueRows(rows)
	if len(rows) > 0 {
		c.AddSpace(styletokens.GapItems(inst.density))
	}
}

// renderDetailMore draws the builder's view of the app in a fold, closed by
// default: the search keywords, the version, where the app comes from, its
// launch kind, its stored keys and the bus subjects behind each line of
// "What it touches". All of it is what keelson('apps') serialises; a person
// picking an app needs none of it, and a person checking one is a click away.
// The fold's open state is shared across apps, so someone reading these
// columns keeps reading them as the selection moves.
func (inst *Inst) renderDetailMore(ids *c.WidgetIdStack, m app.Manifest) {
	rows := make([][2]string, 0, 6)
	if len(m.Keywords) > 0 {
		rows = append(rows, [2]string{"Also found by", strings.Join(m.Keywords, ", ")})
	}
	if m.Version != "" {
		rows = append(rows, [2]string{"Version", m.Version})
	}
	if o, ok := inst.registry.Origin(m.Id); ok {
		if label := originLabel(o); label != "" {
			rows = append(rows, [2]string{"Provided by", label})
		}
	}
	if m.LaunchKind != "" {
		rows = append(rows, [2]string{"Opens with", m.LaunchKind})
	}
	if len(m.PersistedKeys) > 0 {
		rows = append(rows, [2]string{"Stores", strconv.Itoa(len(m.PersistedKeys)) + " state keys"})
	}
	if len(rows) == 0 && len(m.Caps) == 0 {
		return
	}
	for range c.CollapsingHeader(ids.PrepareStr("detail-more"), c.WidgetText().Text("Details").Keep()).KeepIter() {
		renderKeyValueRows(rows)
		if len(m.Caps) > 0 {
			for rt := range c.RichTextLabel("Bus subjects") {
				rt.Small().Weak()
			}
			for _, cap := range m.Caps {
				c.Label(cap.Pattern + " [" + cap.Direction.String() + "]").Send()
			}
		}
	}
}

// renderKeyValueRows draws caption and value pairs, the caption weak and
// first.
func renderKeyValueRows(rows [][2]string) {
	for _, r := range rows {
		for range c.Horizontal().KeepIter() {
			for rt := range c.RichTextLabel(r[0]) {
				rt.Small().Weak()
			}
			c.Label(r[1]).Send()
		}
	}
}

// originLabel renders where an app came from (ADR-0339) as one line: the
// module, its version when the build selected one, and a replacement when
// there is one. "(devel)" is dropped rather than shown: it says the module was
// built from a local directory, which is the absence of a version, not one.
// Without a module the registering package is the best answer there is.
func originLabel(o app.Origin) (label string) {
	if o.Module == "" {
		label = o.Package
		return
	}
	label = o.Module
	if o.ModuleVersion != "" && o.ModuleVersion != "(devel)" {
		label += "@" + o.ModuleVersion
	}
	if o.Replace != "" {
		label += " → " + o.Replace
	}
	return
}

// renderDetailCaps draws "what it touches" from the declared SubjectFilters.
//
// This is the answer §SD6 exists for. Every cap already carries a Reason
// written in reader-facing prose, because ADR-0026 asked authors to justify
// each one to a reviewer — 87 of them in the tree — and a sentence written to
// answer "why does this app need the filesystem" answers "what would opening
// it do" almost verbatim. Nothing had ever rendered them.
//
// The reason is the sentence a person reads; the subject each one stands for
// is what a reader checking it against the cap broker needs, and is listed in
// the Details fold (renderDetailMore) rather than under every reason.
func (inst *Inst) renderDetailCaps(m app.Manifest) {
	if len(m.Caps) == 0 {
		return
	}
	for rt := range c.RichTextLabel("What it touches") {
		rt.Strong()
	}
	for _, cap := range m.Caps {
		reason := cap.Reason
		if reason == "" {
			// A cap with no reason is a manifest that skipped the ADR-0026
			// justification. Showing the bare subject is more honest than
			// hiding the cap: the app does reach that resource.
			reason = "(no reason declared)"
		}
		c.Label("• " + reason).Send()
	}
	c.AddSpace(styletokens.GapItems(inst.density))
}

// hasHelp reports whether the app ships a help corpus the library indexed.
func (inst *Inst) hasHelp(m app.Manifest) (ok bool) {
	if m.Help == nil || inst.lib == nil {
		return
	}
	_, ok = inst.lib.Book(m.Id)
	return
}

// renderDetailHelp lists the app's help documents by title — the contents
// page, not the prose. Each title opens the Help center on that document when
// the host wired a way to say which (SetHelpRef); without one the titles are
// text, since a title that looks like a link and does nothing is worse than a
// list. It answers "is there more to read, and about what" without the
// launcher taking on the markdown renderer's id contract.
func (inst *Inst) renderDetailHelp(ids *c.WidgetIdStack, m app.Manifest) {
	if !inst.hasHelp(m) {
		return
	}
	book, ok := inst.lib.Book(m.Id)
	if !ok {
		return
	}
	docs := book.Docs()
	if len(docs) == 0 {
		return
	}
	for rt := range c.RichTextLabel("Help pages") {
		rt.Strong()
	}
	navigable := inst.helpRef != nil && inst.helpAppId != "" && inst.registry.Launchable(inst.helpAppId)
	for _, d := range docs {
		title := d.Title
		if title == "" {
			title = d.Path
		}
		if !navigable {
			for rt := range c.RichTextLabel("• " + title) {
				rt.Small().Weak()
			}
			continue
		}
		if c.Button(ids.PrepareStr("detail-help-doc-"+d.Path), c.Atoms().Text(icons.PhBookOpen+" "+title).Keep()).
			Kind(c.ButtonKindGhost).
			SendResp().HasPrimaryClicked() {
			inst.openHelp(d.Path, m.Id)
		}
	}
	c.AddSpace(styletokens.GapItems(inst.density))
}

// overviewDoc is the document the Help action opens an app's book on: its
// overview when it has one, else its first document in path order — the
// choice F1 makes (helphost.RefForApp), restated because the launcher does
// not import the help app.
func overviewDoc(lib help.LibraryI, id app.AppIdT) (doc string) {
	if lib == nil {
		return
	}
	b, ok := lib.Book(id)
	if !ok {
		return
	}
	docs := b.Docs()
	if len(docs) == 0 {
		return
	}
	doc = docs[0].Path
	for _, d := range docs {
		if d.Path == "overview" {
			doc = d.Path
			break
		}
	}
	return
}

// openHelp raises the Help center on one document of id's book. The ref is
// queued before the raise, so a fresh window opens on it and an open one
// shows it at its next frame (helphost.PushRef). Without a wired ref the
// Help center opens where it was.
func (inst *Inst) openHelp(doc string, id app.AppIdT) {
	if inst.helpRef != nil && doc != "" {
		inst.helpRef(help.RefT{AppId: id, Doc: doc})
	}
	inst.open(inst.helpAppId)
}
