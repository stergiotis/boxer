package launcher

// The reduced "Apps ▾" menu (ADR-0214 §SD2).
//
// Before this ADR the menu was the second launcher surface: a submenu per
// topic, a provenance filter, and every registered app as a leaf — everything
// the pane had except a search box, which is the one thing a menu cannot
// hold. At 72 apps it was a cascade nobody could scan, and it was the only
// surface reachable while a window was open, so the corpus was browsable
// exactly where it was least usable.
//
// It keeps the two things a menu is good at: the few entries you want again,
// and a door to the surface that can actually search.

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// menuRecentsMax bounds the recents list. Short on purpose: the menu is a
// shortcut, and a shortcut that needs scrolling is the cascade this replaced.
const menuRecentsMax = 8

// RenderMenu draws the top-bar menu. ids must be a stack the caller resets
// before the body's own stack, since the menu renders in the shell chrome
// before the frame body.
func (inst *Inst) RenderMenu(ids *c.WidgetIdStack) {
	for range c.MenuButton(c.Atoms().Text("Apps").Keep()).KeepIter() {
		if inst.registry == nil || len(inst.registry.AllManifests()) == 0 {
			c.Label("(no apps registered)").Send()
			return
		}
		recents := inst.recentManifests()
		for _, m := range recents {
			label := rowLabel(m)
			if m.Icon != "" {
				label = m.Icon + " " + label
			}
			if c.Button(ids.PrepareStr("menu-recent-"+string(m.Id)), c.Atoms().Text(label).Keep()).
				SendResp().HasPrimaryClicked() {
				inst.open(m.Id)
			}
		}
		if len(recents) > 0 {
			c.Separator().Horizontal().Send()
		}
		if c.Button(ids.PrepareStr("menu-browse-all"),
			c.Atoms().Text(icons.PhMagnifyingGlass+" Browse all apps…").Keep()).
			SendResp().HasPrimaryClicked() {
			inst.open(ManifestId)
		}
		// Named so the shortcut is discoverable from the surface a person
		// already found, which is what makes closing the launcher window
		// recoverable (§Consequences).
		for rt := range c.RichTextLabel("or press " + LauncherKeyLabel) {
			rt.Small().Weak()
		}
	}
}

// recentManifests resolves the history provider's app ids to manifests,
// dropping ids the registry no longer holds (an applet the store replaced)
// and the launcher itself — an entry that reopens the window you are
// clicking from is noise.
//
// Empty until a history source is wired, which is also the state of a run
// whose facts store has no history capability. The menu then shows only its
// door, which is honest: nothing has been opened yet that it could offer.
func (inst *Inst) recentManifests() (out []app.Manifest) {
	if inst.recentFn == nil {
		return
	}
	ids := inst.recentFn()
	out = make([]app.Manifest, 0, min(len(ids), menuRecentsMax))
	for _, id := range ids {
		if len(out) >= menuRecentsMax {
			return
		}
		if id == ManifestId {
			continue
		}
		m, ok := inst.registry.LookupManifest(id)
		if !ok {
			continue
		}
		out = append(out, m)
	}
	return
}
