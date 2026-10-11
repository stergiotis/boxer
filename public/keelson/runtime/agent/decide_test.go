package agent

import (
	"testing"

	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// The classifiers as they stood before the decision table, kept here as
// the oracle: the table has to give the same answers over the whole input
// space.

func legacyClassifyOperation(c *Ceiling, e *GrantedEntry, op string, effect app.OperationEffectE) (s CellStatusE) {
	switch {
	case c.refuseEffect(effect) != "":
		return CellStatusAboveCeiling
	case e == nil || !e.Covers(op):
		return CellStatusNotGranted
	case effect != app.OperationEffectNone && e.Mode == ModeObserve:
		return CellStatusObserveOnly
	}
	return CellStatusGranted
}

func legacyClassifyLaunch(c *Ceiling, l *GrantLaunch) (s CellStatusE) {
	switch {
	case c.refuseLaunch() != "":
		return CellStatusAboveCeiling
	case l == nil || l.Count == 0:
		return CellStatusNotGranted
	}
	return CellStatusGranted
}

func legacyClassifyArrange(c *Ceiling, desktop ModeE) (s CellStatusE) {
	switch {
	case c.refuseDesktop() != "":
		return CellStatusAboveCeiling
	case desktop == ModeUnspecified:
		return CellStatusNotGranted
	case desktop != ModeAct:
		return CellStatusObserveOnly
	}
	return CellStatusGranted
}

func legacyClassifyWindowVerb(c *Ceiling, e *GrantedEntry) (s CellStatusE) {
	switch {
	case e == nil:
		return CellStatusNotGranted
	case e.Mode == ModeAct && c.refuseMode(ModeAct) != "":
		return CellStatusAboveCeiling
	case e.Mode != ModeAct:
		return CellStatusObserveOnly
	}
	return CellStatusGranted
}

// The window-verb check as the dispatcher wrote it before the table, over
// a task's effective mode; the table's reading through refuseMode has to
// agree with it.
func legacyWindowVerbRefused(c *Ceiling, e *entry) (refused bool) {
	t := &task{ceiling: c}
	return e.mode == ModeAct && t.modeOf(e) != ModeAct
}

func genCeiling(t *rapid.T) *Ceiling {
	if rapid.Bool().Draw(t, "nilCeiling") {
		return nil
	}
	return &Ceiling{
		Mode:    ModeE(rapid.IntRange(int(ModeUnspecified), int(ModeAct)).Draw(t, "mode")),
		Effect:  app.OperationEffectE(rapid.IntRange(int(app.OperationEffectNone)-1, int(app.OperationEffectConsequential)+1).Draw(t, "effect")),
		Launch:  rapid.Bool().Draw(t, "launch"),
		Desktop: rapid.Bool().Draw(t, "desktop"),
		Reach:   ReachE(rapid.IntRange(0, int(ReachNetwork)).Draw(t, "reach")),
	}
}

func genEntry(t *rapid.T) *GrantedEntry {
	if rapid.Bool().Draw(t, "nilEntry") {
		return nil
	}
	return &GrantedEntry{Mode: ModeE(rapid.IntRange(int(ModeUnspecified), int(ModeAct)).Draw(t, "entryMode")),
		Ops: rapid.SliceOfN(rapid.SampledFrom([]string{"get", "set", "run"}), 0, 3).Draw(t, "ops")}
}

func TestTheDecisionTableAgreesWithTheClassifiers(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		c := genCeiling(t)
		e := genEntry(t)
		op := rapid.SampledFrom([]string{"get", "set", "run", "other"}).Draw(t, "op")
		effect := app.OperationEffectE(rapid.IntRange(int(app.OperationEffectNone), int(app.OperationEffectConsequential)).Draw(t, "opEffect"))
		if got, want := ClassifyOperation(c, e, op, effect), legacyClassifyOperation(c, e, op, effect); got != want {
			t.Fatalf("operation: table %s, classifier %s", got, want)
		}
		var l *GrantLaunch
		if rapid.Bool().Draw(t, "hasLaunch") {
			l = &GrantLaunch{Count: uint32(rapid.IntRange(0, 3).Draw(t, "count"))}
		}
		if got, want := ClassifyLaunch(c, l), legacyClassifyLaunch(c, l); got != want {
			t.Fatalf("launch: table %s, classifier %s", got, want)
		}
		desktop := ModeE(rapid.IntRange(int(ModeUnspecified), int(ModeAct)).Draw(t, "desktopMode"))
		if got, want := ClassifyArrange(c, desktop), legacyClassifyArrange(c, desktop); got != want {
			t.Fatalf("arrange: table %s, classifier %s", got, want)
		}
		if got, want := ClassifyWindowVerb(c, e), legacyClassifyWindowVerb(c, e); got != want {
			t.Fatalf("window verb: table %s, classifier %s", got, want)
		}
		if e != nil {
			refused := decideWindowVerb(c, true, e.Mode) == decisionAboveCeiling
			if want := legacyWindowVerbRefused(c, &entry{mode: e.Mode}); refused != want {
				t.Fatalf("window verb through the effective mode: table %t, dispatcher %t", refused, want)
			}
		}
	})
}

// A spent launch is refused at the dispatcher and shown granted on the
// surface: the surface shows the grant, the calls its use.
func TestASpentLaunchIsRefusedAndShownGranted(t *testing.T) {
	c := Unlimited()
	if d := decideLaunch(&c, true, 2, 2); d != decisionSpent {
		t.Fatalf("spent: %d", d)
	}
	if s := decisionSpent.cellStatus(); s != CellStatusGranted {
		t.Fatalf("spent on the surface: %s", s)
	}
}
