package jackstay

import (
	"fmt"
	"time"

	"github.com/stergiotis/boxer/public/hmi/progressest"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fsmview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/inspector"
)

// phaseE is where the plan stands between the two servers: what the last
// step that finished left, or the step running now. It is a different thing
// from the page on screen — the breadcrumb says where the operator is, the
// phase says what the plan has reached — so a page can be revisited without
// moving it.
//
// The window never drives the machine: each frame observes the phase from
// the jobs and the plan (observePhase) and mirrors it, as play does for its
// query result. The rules below draw the graph the state chip opens and name
// the edges an operator causes; an edge they do not declare is followed, and
// graded in the log by mirrorPhase.
type phaseE uint8

const (
	phaseIdle        phaseE = iota // no servers discovered, no plan opened
	phaseDiscovering               // reading both servers' system tables, no plan open
	phaseDiscovered                // servers known, no plan yet
	phaseOpening                   // a plan is read from the data area, or imported
	phasePlanning                  // judging the tables, or running their DDL on the target
	phaseDDLPending                // a plan whose target needs DDL before a diff or a sync
	phasePlanned                   // a plan whose target structure is in line
	phaseComparing                 // the content diff runs
	phaseDiffers                   // the latest comparison found differing tables
	phaseIdentical                 // the latest comparison found every compared table identical
	phaseSyncing                   // the sync runs
	phaseSynced                    // the latest sync verified every chunk it copied
	phaseIncomplete                // the latest sync left chunks unsynced, or stopped
	phaseStale                     // the servers moved under the plan
)

var allPhases = []phaseE{
	phaseIdle, phaseDiscovering, phaseDiscovered, phaseOpening, phasePlanning, phaseDDLPending, phasePlanned,
	phaseComparing, phaseDiffers, phaseIdentical, phaseSyncing, phaseSynced, phaseIncomplete, phaseStale,
}

func (inst phaseE) String() (s string) {
	switch inst {
	case phaseIdle:
		return "idle"
	case phaseDiscovering:
		return "discovering"
	case phaseDiscovered:
		return "discovered"
	case phaseOpening:
		return "opening"
	case phasePlanning:
		return "planning"
	case phaseDDLPending:
		return "ddl pending"
	case phasePlanned:
		return "planned"
	case phaseComparing:
		return "comparing"
	case phaseDiffers:
		return "differs"
	case phaseIdentical:
		return "identical"
	case phaseSyncing:
		return "syncing"
	case phaseSynced:
		return "synced"
	case phaseIncomplete:
		return "incomplete"
	case phaseStale:
		return "stale"
	}
	return "?"
}

// observePhase derives the phase from this frame's jobs and plan, with no
// memory of its own beyond whether the latest sync stopped. A running job
// outranks what the plan holds; a plan the servers moved under outranks its
// sections. Discovering the servers again while a plan is open does not move
// the plan, so it shows only before there is one. Between a diff and a sync
// the later one speaks: a sync drops the diffs of the tables it copied, but a
// comparison after a sync leaves the reports in place.
func (inst *App) observePhase() (ph phaseE) {
	switch {
	case inst.syncJob.Running():
		return phaseSyncing
	case inst.diffJob.Running():
		return phaseComparing
	case inst.structureJob.Running():
		return phasePlanning
	case inst.isOpening():
		return phaseOpening
	case inst.discoverJob.Running() && inst.plan == nil:
		return phaseDiscovering
	case len(inst.stale) > 0:
		return phaseStale
	}
	p := inst.plan
	if p == nil {
		if inst.disc != nil {
			return phaseDiscovered
		}
		return phaseIdle
	}
	var diffAt, syncAt time.Time
	unsynced := 0
	for _, t := range p.Tables {
		if d := t.Diff; d != nil && d.ComputedAt.After(diffAt) {
			diffAt = d.ComputedAt
		}
		if r := t.SyncReport; r != nil {
			unsynced += r.Failed + r.Stale
			if r.FinishedAt.After(syncAt) {
				syncAt = r.FinishedAt
			}
		}
	}
	// A sync that stopped may have finished no table, so its report-less
	// end counts as the sync's time.
	stopped := inst.syncStopped
	if stopped && inst.syncFinished.After(syncAt) {
		syncAt = inst.syncFinished
	}
	if !syncAt.IsZero() && !syncAt.Before(diffAt) {
		if stopped || unsynced > 0 {
			return phaseIncomplete
		}
		return phaseSynced
	}
	if same, differ := inst.diffCounts(); differ > 0 {
		return phaseDiffers
	} else if same > 0 {
		return phaseIdentical
	}
	if p.HasPendingDDL() {
		return phaseDDLPending
	}
	return phasePlanned
}

// isOpening reports whether a plan is being opened or imported; an export
// leaves the plan where it is.
func (inst *App) isOpening() (opening bool) {
	return inst.fileJob.Running() && inst.fileOp != fileOpExport
}

// mirrorPhase moves the machine to the phase this frame observed. A frame
// samples the jobs, so a step that starts and finishes between two frames
// shows as an edge that skips its running phase; that is the declared path
// taken unwatched and logs at debug. An observation no declared path reaches
// contradicts the model and logs a warning.
func (inst *App) mirrorPhase() {
	m := inst.phaseMachine
	cur, obs := m.Current(), inst.observePhase()
	if cur == obs || m.Mirror(obs) {
		return
	}
	ev := inst.logger.Warn()
	msg := "jackstay: the phase moved along an edge the declared graph cannot reach (mirrored)"
	if m.CanReach(cur, obs) {
		ev = inst.logger.Debug()
		msg = "jackstay: the phase skipped states no frame sampled (mirrored)"
	}
	ev.Stringer("from", cur).Stringer("to", obs).Msg(msg)
}

// newPhaseMachine declares the plan's lifecycle: discover, plan the
// structure, apply its DDL, compare, sync. Every settled phase with a plan
// can be planned again or give way to another plan opened, and a stale plan
// only so; Compare and Start stay disabled while it is stale. A sync that
// finds nothing to copy leaves the plan where it was. A failed or cancelled
// step returns to the phase it started from, so failure needs no state of
// its own; the step's page says why, and the graph reaches that phase along
// declared edges (CanReach), which mirrorPhase logs at debug.
func newPhaseMachine() (m *fsmview.Machine[phaseE]) {
	m = fsmview.NewMachine(phaseIdle, 64, fsmview.MachineOptions[phaseE]{
		Label:      func(ph phaseE) string { return ph.String() },
		StateOrder: allPhases,
		StateColor: phaseColor,
	})
	m.AddRule(phaseIdle, phaseDiscovering, phaseOpening).
		AddRule(phaseDiscovering, phaseDiscovered).
		AddRule(phaseDiscovered, phaseDiscovering, phasePlanning, phaseOpening).
		AddRule(phaseOpening, phaseIdle, phaseDDLPending, phasePlanned, phaseDiffers, phaseIdentical, phaseSynced, phaseIncomplete).
		AddRule(phasePlanning, phaseDDLPending, phasePlanned, phaseStale).
		AddRule(phaseDDLPending, phasePlanning, phaseComparing, phaseSyncing, phaseOpening).
		AddRule(phasePlanned, phasePlanning, phaseComparing, phaseSyncing, phaseOpening).
		AddRule(phaseComparing, phaseDiffers, phaseIdentical, phaseStale).
		AddRule(phaseDiffers, phaseComparing, phaseSyncing, phasePlanning, phaseOpening).
		AddRule(phaseIdentical, phaseComparing, phaseSyncing, phasePlanning, phaseOpening).
		AddRule(phaseSyncing, phaseSynced, phaseIncomplete, phaseStale, phaseDDLPending, phasePlanned, phaseDiffers, phaseIdentical).
		AddRule(phaseSynced, phaseComparing, phaseSyncing, phasePlanning, phaseOpening).
		AddRule(phaseIncomplete, phaseSyncing, phaseComparing, phasePlanning, phaseOpening).
		AddRule(phaseStale, phasePlanning, phaseOpening).
		EdgeLabel(phaseIdle, phaseDiscovering, "Discover").
		EdgeLabel(phaseIdle, phaseOpening, "open a plan").
		EdgeLabel(phaseDiscovering, phaseDiscovered, "inventories").
		EdgeLabel(phaseDiscovered, phasePlanning, "Plan the structure").
		EdgeLabel(phaseOpening, phaseIdle, "nothing opened").
		EdgeLabel(phasePlanning, phaseDDLPending, "DDL needed").
		EdgeLabel(phasePlanning, phasePlanned, "in line").
		EdgeLabel(phasePlanning, phaseStale, "servers moved").
		EdgeLabel(phaseDDLPending, phasePlanning, "Run on the target").
		EdgeLabel(phasePlanned, phaseComparing, "Compare content").
		EdgeLabel(phasePlanned, phaseSyncing, "Start").
		EdgeLabel(phaseComparing, phaseDiffers, "differences").
		EdgeLabel(phaseComparing, phaseIdentical, "none").
		EdgeLabel(phaseComparing, phaseStale, "servers moved").
		EdgeLabel(phaseDiffers, phaseSyncing, "Start").
		EdgeLabel(phaseIdentical, phaseSyncing, "Start").
		EdgeLabel(phaseSyncing, phaseSynced, "verified").
		EdgeLabel(phaseSyncing, phaseIncomplete, "chunks left, or stopped").
		EdgeLabel(phaseSyncing, phaseStale, "servers moved").
		EdgeLabel(phaseSynced, phaseComparing, "Compare content again").
		EdgeLabel(phaseIncomplete, phaseSyncing, "resume").
		EdgeLabel(phaseStale, phasePlanning, "Plan the structure")
	for _, ph := range []phaseE{phaseDDLPending, phasePlanned, phaseDiffers, phaseIdentical} {
		m.EdgeLabel(phaseSyncing, ph, "nothing to sync")
	}
	return
}

// phaseColor tints the graph's nodes: the running phases in the accent, the
// outcomes by severity, the rest muted.
func phaseColor(ph phaseE, _ bool) styletokens.RGBA8 {
	switch ph {
	case phaseDiscovering, phaseOpening, phasePlanning, phaseComparing, phaseSyncing:
		return styletokens.AccentDefault
	case phaseSynced, phaseIdentical:
		return styletokens.SuccessDefault
	case phaseDDLPending, phaseDiffers:
		return styletokens.WarningDefault
	case phaseIncomplete, phaseStale:
		return styletokens.ErrorDefault
	case phaseDiscovered, phasePlanned:
		return styletokens.NeutralDefault
	}
	return styletokens.NeutralSubtle
}

// phaseTone is phaseColor for the chip's badge.
func phaseTone(ph phaseE) badge.ToneE {
	switch ph {
	case phaseDiscovering, phaseOpening, phasePlanning, phaseComparing, phaseSyncing:
		return badge.TonePrimary
	case phaseSynced, phaseIdentical:
		return badge.ToneSuccess
	case phaseDDLPending, phaseDiffers:
		return badge.ToneWarning
	case phaseIncomplete, phaseStale:
		return badge.ToneError
	}
	return badge.ToneNeutral
}

// renderPhase draws the state chip with its one-line summary. The chip opens
// the inspector: the graph, the history of phases and where they come from.
func (inst *App) renderPhase() {
	inst.phaseChip.Opts.Provenance = inspector.Provenance{
		Subject:   "app.jackstay.plan.phase",
		SourceApp: string(AppId),
	}
	inst.phaseChip.Opts.Summary = func() {
		s := inst.phaseLine()
		if s == "" {
			return
		}
		muted := color.Hex(styletokens.NeutralTextSecondary.AsHex())
		c.LabelAtoms(c.Atoms().BeginRichTextColored(muted, color.Transparent, s).Small().End().Keep()).Send()
	}
	inst.phaseChip.Render()
}

// phaseLine is the chip's summary: what the phase holds, in numbers.
func (inst *App) phaseLine() (s string) {
	p := inst.plan
	switch inst.phaseMachine.Current() {
	case phaseIdle:
		return "name the servers and discover them, or open a plan"
	case phaseDiscovering:
		return "reading both servers' system tables…"
	case phaseDiscovered:
		return hostLabel(inst.disc.srcEp.URL) + " → " + hostLabel(inst.disc.dstEp.URL) + "; choose the databases"
	case phaseOpening:
		return "reading the plan…"
	case phasePlanning:
		return "judging the tables, or running their DDL on the target…"
	case phaseDDLPending:
		return plural(len(p.Tables), "table") + "; the target needs DDL first"
	case phasePlanned:
		return plural(len(p.Tables), "table") + "; the target's structure is in line"
	case phaseComparing:
		return "scanning both sides for digests…"
	case phaseDiffers:
		same, differ := inst.diffCounts()
		return fmt.Sprintf("%d of %s differ", differ, plural(same+differ, "compared table"))
	case phaseIdentical:
		same, _ := inst.diffCounts()
		return plural(same, "compared table") + ", all identical"
	case phaseSyncing:
		return fmt.Sprintf("%d rows landed, %s on the wire", inst.rows.Load(), progressest.FormatBytes(inst.bytes.Load()))
	case phaseSynced, phaseIncomplete:
		var rows uint64
		unsynced := 0
		for _, t := range p.Tables {
			if r := t.SyncReport; r != nil {
				rows += r.Rows
				unsynced += r.Failed + r.Stale
			}
		}
		s = plural(int(rows), "row") + " copied"
		if unsynced > 0 {
			s += ", " + plural(unsynced, "chunk") + " not synced"
		} else if inst.syncStopped {
			s += ", the sync stopped"
		}
		return
	case phaseStale:
		return "the servers moved under the plan; plan the structure again"
	}
	return ""
}
