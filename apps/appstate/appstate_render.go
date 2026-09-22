package appstate

import (
	"fmt"
	"time"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist/persiststore"
	"github.com/stergiotis/boxer/public/observability/humanfmt"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
)

// leftWidth is the app list's first width; the panel is resizable.
const leftWidth = 340

func (inst *App) render() {
	snap := inst.snapshot()
	apps := summarize(snap.rows)
	for range c.PanelTopInside(inst.ids.PrepareStr("top")).Resizable(false).KeepIter() {
		inst.renderStatus(snap, apps)
		inst.renderConfirm()
	}
	for range c.PanelLeftInside(inst.ids.PrepareStr("apps")).Resizable(true).DefaultSize(leftWidth).KeepIter() {
		for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
			inst.renderApps(apps)
		}
	}
	for range c.PanelCentralInside().KeepIter() {
		for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
			inst.renderEntries(snap)
		}
	}
}

func (inst *App) renderStatus(snap snapshot, apps []appSummary) {
	for range c.HorizontalTop().KeepIter() {
		if c.Button(inst.ids.PrepareStr("refresh"), c.Atoms().Text("Refresh").Keep()).SendResp().HasPrimaryClicked() {
			inst.markDirty()
		}
		switch {
		case !snap.endpoint && snap.lastError != "":
			c.Label(snap.lastError).Send()
		case snap.refreshed.IsZero():
			c.Label("Reading keelson('app_state')…").Send()
		default:
			c.Label(fmt.Sprintf("%d entries across %d apps, read %s ago",
				len(snap.rows), len(apps), time.Since(snap.refreshed).Round(time.Second))).Send()
		}
	}
	for range c.HorizontalTop().KeepIter() {
		switch {
		case snap.lastError != "" && snap.endpoint:
			badge.New(inst.ids.PrepareStr("err"), snap.lastError).Tone(badge.ToneError).Variant(badge.VariantSoft).Send()
		case snap.inflight > 0:
			badge.New(inst.ids.PrepareStr("busy"), fmt.Sprintf("%d request(s) in flight", snap.inflight)).Tone(badge.ToneInfo).Variant(badge.VariantSoft).Send()
		case snap.lastNote != "":
			badge.New(inst.ids.PrepareStr("note"), snap.lastNote).Tone(badge.ToneSuccess).Variant(badge.VariantSoft).Send()
		default:
			c.LabelAtoms(c.Atoms().BeginRichText("A clear appends a tombstone: the app starts from its defaults, and the trail keeps the old rows.").Weak().End().Keep()).Send()
		}
	}
}

// renderConfirm is forget's confirmation step (ADR-0185 M3): the banner
// names the app and the count armed, and only its own button clears.
func (inst *App) renderConfirm() {
	armed := inst.armed
	if armed == nil {
		return
	}
	c.AddSpace(styletokens.PaddingInner(inst.density))
	for range c.HorizontalTop().KeepIter() {
		badge.New(inst.ids.PrepareStr("confirm-msg"),
			fmt.Sprintf("Forget all %d entries %s keeps? It starts from its defaults at its next open.", armed.entries, shortApp(armed.appId))).
			Tone(badge.ToneWarning).Variant(badge.VariantSoft).Send()
		if c.Button(inst.ids.PrepareStr("confirm-forget"), c.Atoms().Text(fmt.Sprintf("Forget %d entries", armed.entries)).Keep()).SendResp().HasPrimaryClicked() {
			inst.confirmForget()
		}
		if c.Button(inst.ids.PrepareStr("confirm-cancel"), c.Atoms().Text("Cancel").Keep()).SendResp().HasPrimaryClicked() {
			inst.cancelForget()
		}
	}
}

func (inst *App) renderApps(apps []appSummary) {
	if len(apps) == 0 {
		c.Label("No app keeps any state.").Send()
		return
	}
	for range c.Grid(inst.ids.PrepareStr("apps-grid")).NumColumns(4).Striped(true).KeepIter() {
		for _, h := range []string{"app", "entries", "size", ""} {
			strong(h)
		}
		c.EndRow()
		for _, s := range apps {
			for range c.IdScope(inst.ids.PrepareStr(s.appId)) {
				clicked := false
				for range c.HoverText(s.appId).KeepIter() {
					clicked = c.SelectableLabel(inst.ids.PrepareStr("name"), inst.selected == s.appId, shortApp(s.appId)).SendResp().HasPrimaryClicked()
				}
				if clicked {
					inst.selected = s.appId
				}
				mono(fmt.Sprint(s.entries))
				mono(humanfmt.Bytes(uint64(s.bytes)))
				if c.Button(inst.ids.PrepareStr("forget"), c.Atoms().Text("Forget…").Keep()).Small().SendResp().HasPrimaryClicked() {
					inst.armForget(s)
				}
			}
			c.EndRow()
		}
	}
}

func (inst *App) renderEntries(snap snapshot) {
	if inst.selected == "" {
		c.Label("Select an app to see what it keeps.").Send()
		return
	}
	rows := entriesOf(snap.rows, inst.selected)
	c.LabelAtoms(c.Atoms().BeginRichText(inst.selected).Monospace().Heading().End().Keep()).Send()
	if len(rows) == 0 {
		c.Label("It keeps nothing now.").Send()
		return
	}
	c.AddSpace(styletokens.PaddingInner(inst.density))
	for range c.Grid(inst.ids.PrepareStr("entries-grid")).NumColumns(6).Striped(true).KeepIter() {
		for _, h := range []string{"kind", "key", "size", "detail", "written", ""} {
			strong(h)
		}
		c.EndRow()
		for _, r := range rows {
			for range c.IdScope(inst.ids.PrepareStr(r.EntityId)) {
				badge.New(inst.ids.PrepareStr("kind"), r.Kind).Tone(kindTone(r.Kind)).Variant(badge.VariantSoft).Size(badge.SizeSm).Send()
				mono(r.Key)
				mono(sizeOf(r))
				c.Label(r.Detail).Send()
				mono(r.WrittenAt)
				if c.Button(inst.ids.PrepareStr("delete"), c.Atoms().Text("Delete").Keep()).Small().SendResp().HasPrimaryClicked() {
					inst.deleteEntry(r)
				}
			}
			c.EndRow()
		}
	}
}

// sizeOf is a payload's size; a column width has no payload, only fields.
func sizeOf(r entryRow) string {
	if r.Kind == persiststore.KindColumnWidth {
		return "—"
	}
	return humanfmt.Bytes(uint64(r.PayloadBytes))
}

// kindTone tells the kinds apart; an unknown kind is the one to look at.
func kindTone(kind string) badge.ToneE {
	switch kind {
	case persiststore.KindPersist:
		return badge.TonePrimary
	case persiststore.KindWorkingset:
		return badge.ToneInfo
	case persiststore.KindColumnWidth:
		return badge.ToneNeutral
	}
	return badge.ToneWarning
}

func strong(text string) {
	c.LabelAtoms(c.Atoms().BeginRichText(text).Strong().End().Keep()).Send()
}

func mono(text string) {
	c.LabelAtoms(c.Atoms().BeginRichText(text).Monospace().End().Keep()).Send()
}
