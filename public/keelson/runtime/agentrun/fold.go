package agentrun

// The resume fold (ADR-0308 §SD4; ADR-0303 §SD3, superseded by ADR-0308):
// what a run's journal
// says about acts begun and not settled, read in order and settled each by
// its kind and effect class. The fold decides nothing about the model or
// the apps; it reports, and the coordinator acts on the report.

// The effect classes the fold reads on an intent, spelled as
// app.OperationSpec.Effect does.
const (
	EffectNone          = "none"
	EffectView          = "view"
	EffectDocument      = "document"
	EffectRun           = "run"
	EffectConsequential = "consequential"
)

// ResolutionE is what a resume does with one unsettled act.
type ResolutionE uint8

const (
	ResolutionUnspecified ResolutionE = iota
	// ResolutionRemake: a request without a reply; the model call is made
	// again.
	ResolutionRemake
	// ResolutionNotCompleted: a query without an outcome; the model is told
	// the call did not complete and may read again.
	ResolutionNotCompleted
	// ResolutionUnknown: a command without an outcome; never re-sent, the
	// model is told the outcome is unknown.
	ResolutionUnknown
	// ResolutionListToPerson: a consequential command without an outcome;
	// never re-sent, listed to the person before anything else happens.
	ResolutionListToPerson
	// ResolutionAskAgain: a question to the person without an answer; shown
	// again.
	ResolutionAskAgain
	// ResolutionLapsed: a grant, widening or confirmation without a
	// decision; asked again as part of the new task.
	ResolutionLapsed
	// ResolutionPending: a command applied whose asynchronous completion
	// was not journaled (ADR-0315 §SD6): "applied, not completed".
	ResolutionPending
)

var resolutionNames = []string{"unspecified", "remake", "not-completed", "unknown", "list-to-person", "ask-again", "lapsed", "pending"}

func (inst ResolutionE) String() (s string) {
	if int(inst) < len(resolutionNames) {
		return resolutionNames[inst]
	}
	return "unknown"
}

// Unsettled is one act begun and not settled.
type Unsettled struct {
	Entry      Entry
	Resolution ResolutionE
}

// Resume is the fold's report.
type Resume struct {
	// Unsettled are the acts of the open turn to settle, in journal
	// order; a turn entry settles its turn's for good.
	Unsettled []Unsettled
	// LastRequest is the last request entry, for the caller that pins a
	// resume to the projection it rebuilds (ADR-0308 §SD5, law L4): its
	// body carries the digest of the request as sent, to compare with the
	// projection's. The fold reports it; the comparison is the caller's.
	// Nil when none.
	LastRequest *Entry
	// Label is the join of every entry's label: what the run has read.
	Label Label
	// Turns counts the turn entries, OpenTurn is the turn of the last
	// entry when no turn entry closed it.
	Turns    int
	OpenTurn string
	// Deleted says the run is tombstoned and nothing resumes.
	Deleted bool
}

// FormPending marks an outcome whose command returned a handle for
// asynchronous work; a completed entry with the same key settles it.
const FormPending = "pending"

// FormDrop marks an edit that removes its target from the projection
// rather than replacing its body.
const FormDrop = "drop"

// FormGrant marks an asked entry that is a grant, widening or
// confirmation rather than ask_user, which lapses rather than repeats.
const FormGrant = "grant"

// Fold reads entries in order and reports what resume must settle.
func Fold(entries []Entry) (r Resume) {
	replies := make(map[string]bool)
	outcomes := make(map[string]Entry)
	completed := make(map[string]bool)
	answered := make(map[string]bool)
	for _, e := range entries {
		switch e.Kind {
		case KindReply:
			replies[e.Key] = true
		case KindOutcome:
			outcomes[e.Key] = e
		case KindCompleted:
			completed[e.Key] = true
		case KindAnswered:
			answered[e.Key] = true
		case KindDeleted:
			r.Deleted = true
		}
	}
	// Only the open turn's acts are unsettled: a turn entry closes its
	// turn with whatever did not settle in it, which resume does not
	// remake or list again.
	turnOpen := ""
	for i := range entries {
		e := entries[i]
		r.Label = r.Label.Join(e.Label)
		if e.Turn != "" {
			turnOpen = e.Turn
		}
		switch e.Kind {
		case KindTurn:
			r.Turns++
			turnOpen = ""
			r.Unsettled = nil
		case KindRequest:
			r.LastRequest = &entries[i]
			if !replies[e.Key] {
				r.Unsettled = append(r.Unsettled, Unsettled{Entry: e, Resolution: ResolutionRemake})
			}
		case KindIntent:
			out, settled := outcomes[e.Key]
			switch {
			case settled && out.Form == FormPending && !completed[e.Key]:
				r.Unsettled = append(r.Unsettled, Unsettled{Entry: e, Resolution: ResolutionPending})
			case settled:
			case e.Effect == EffectConsequential:
				r.Unsettled = append(r.Unsettled, Unsettled{Entry: e, Resolution: ResolutionListToPerson})
			case e.Effect == EffectDocument || e.Effect == EffectRun:
				r.Unsettled = append(r.Unsettled, Unsettled{Entry: e, Resolution: ResolutionUnknown})
			default:
				r.Unsettled = append(r.Unsettled, Unsettled{Entry: e, Resolution: ResolutionNotCompleted})
			}
		case KindAsked:
			if answered[e.Key] {
				continue
			}
			if e.Form == FormGrant {
				r.Unsettled = append(r.Unsettled, Unsettled{Entry: e, Resolution: ResolutionLapsed})
			} else {
				r.Unsettled = append(r.Unsettled, Unsettled{Entry: e, Resolution: ResolutionAskAgain})
			}
		}
	}
	r.OpenTurn = turnOpen
	return
}
