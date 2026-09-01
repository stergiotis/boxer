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
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
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
		inst.renderDetailHelp(m)
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
		if m.Kind != app.KindApp {
			badge.New(ids.PrepareStr("detail-kind"), m.Kind.String()).
				Size(badge.SizeSm).
				Send()
		}
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
// because they were previously unreachable from any launcher surface.
func (inst *Inst) renderDetailActions(ids *c.WidgetIdStack, m app.Manifest) {
	_, isOpen := inst.openAppSet()[m.Id]
	label := icons.PhArrowSquareOut + " Open"
	if isOpen {
		label = icons.PhArrowSquareOut + " Raise"
	}
	for range c.Horizontal().KeepIter() {
		if c.Button(ids.PrepareStr("detail-open"), c.Atoms().Text(label).Keep()).
			SendResp().HasPrimaryClicked() {
			inst.open(m.Id)
		}
		if inst.helpAppId != "" && inst.hasHelp(m) {
			if c.Button(ids.PrepareStr("detail-help"), c.Atoms().Text(icons.PhBookOpen+" Help").Keep()).
				SendResp().HasPrimaryClicked() {
				// The Help center reads its own selection from the library, so
				// raising it is the whole action here. Selecting this app's
				// book inside it wants a cross-app selection verb the help
				// host does not expose yet.
				inst.open(inst.helpAppId)
			}
		}
	}
	c.AddSpace(styletokens.GapItems(inst.density))
}

// renderDetailMeta draws the key-value block: the classification and
// declaration columns keelson('apps') carries.
//
// Rows appear only when they hold something. A pane that renders eighteen
// labels for an app that declares three of them is the failure on the other
// side of the one-line row (§SD10) — the point of a detail pane is that the
// expensive information renders once, not that all of it renders always.
func (inst *Inst) renderDetailMeta(m app.Manifest) {
	rows := make([][2]string, 0, 8)
	if len(m.Topics) > 0 {
		labels := make([]string, 0, len(m.Topics))
		for _, t := range m.Topics {
			labels = append(labels, topicLabel(t))
		}
		rows = append(rows, [2]string{"Topics", strings.Join(labels, " · ")})
	}
	if len(m.Keywords) > 0 {
		rows = append(rows, [2]string{"Also found by", strings.Join(m.Keywords, ", ")})
	}
	if m.Version != "" {
		rows = append(rows, [2]string{"Version", m.Version})
	}
	if m.LaunchKind != "" {
		rows = append(rows, [2]string{"Opens with", m.LaunchKind})
	}
	if m.Workingset {
		rows = append(rows, [2]string{"Remembers", "reopens with the state you left"})
	}
	if len(m.PersistedKeys) > 0 {
		rows = append(rows, [2]string{"Stores", strconv.Itoa(len(m.PersistedKeys)) + " state keys"})
	}
	if len(rows) == 0 {
		return
	}
	for _, r := range rows {
		for range c.Horizontal().KeepIter() {
			for rt := range c.RichTextLabel(r[0]) {
				rt.Small().Weak()
			}
			c.Label(r[1]).Send()
		}
	}
	c.AddSpace(styletokens.GapItems(inst.density))
}

// renderDetailCaps draws "what it touches" from the declared SubjectFilters.
//
// This is the answer §SD6 exists for. Every cap already carries a Reason
// written in reader-facing prose, because ADR-0026 asked authors to justify
// each one to a reviewer — 87 of them in the tree — and a sentence written to
// answer "why does this app need the filesystem" answers "what would opening
// it do" almost verbatim. Nothing had ever rendered them.
//
// The pattern is shown beside the reason rather than instead of it: the reason
// is the sentence, and the subject is what a reader who wants to check it
// against the cap broker needs.
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
		for rt := range c.RichTextLabel("    " + cap.Pattern + " [" + cap.Direction.String() + "]") {
			rt.Small().Weak()
		}
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
// page, not the prose. It answers "is there more to read, and about what"
// without the launcher taking on the markdown renderer's id contract.
func (inst *Inst) renderDetailHelp(m app.Manifest) {
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
	for _, d := range docs {
		title := d.Title
		if title == "" {
			title = d.Path
		}
		for rt := range c.RichTextLabel("• " + title) {
			rt.Small().Weak()
		}
	}
}
