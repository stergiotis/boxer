package chat

import (
	"context"
	"encoding/json/v2"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/projection"
)

// Sealed data and the conversation: a window's label is the highest of what
// it read, and a query of a labelled window hands that label to the
// conversation for good. The model is told before the first such read of
// each window, while the conversation can still choose a capture instead;
// the export refusal later names the call that did it.

// sealedReadHold holds back the first read of a window that holds sealed
// data, with what the read would cost: the answer the model reads instead,
// empty when the read may go ahead. Repeating the call goes through. A
// window list the host cannot give holds nothing back: the label still
// lands when the read answers.
func (inst *coordinator) sealedReadHold(ctx context.Context, h string, windows []uint64) (answer string) {
	inst.mu.Lock()
	confined := inst.confined
	inst.mu.Unlock()
	if confined {
		return ""
	}
	listed, err := inst.cli.List(ctx, h)
	if err != nil {
		return ""
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	var sealed []string
	for _, w := range listed {
		if !w.Confined || inst.sealedWarned[w.Instance] {
			continue
		}
		for _, asked := range windows {
			if asked == w.Instance {
				if inst.sealedWarned == nil {
					inst.sealedWarned = make(map[uint64]bool, 2)
				}
				inst.sealedWarned[w.Instance] = true
				sealed = append(sealed, strconv.FormatUint(w.Instance, 10))
			}
		}
	}
	if len(sealed) == 0 {
		return ""
	}
	b, _ := json.Marshal(callOutcome{Phase: "held", Reason: "window " + strings.Join(sealed, ", ") +
		" holds sealed data. Reading it confines this conversation: later model calls go only to a local or trusted endpoint, and the artefact cannot be exported to the lading store. artefact_capture shows the window without that. Repeat the call to read it anyway."})
	return string(b)
}

// confinedBy names the first tool call whose result carried the confined
// tier, as the journal has it, and gives that call's key, which its intent
// and outcome entries share; both empty when the journal names none.
func (inst *coordinator) confinedBy() (what string, key string) {
	w := inst.journalNow()
	if w == nil {
		return "", ""
	}
	entries := w.Entries()
	intents := make(map[string]agentrun.Entry, 8)
	for _, e := range entries {
		if e.Kind == agentrun.KindIntent {
			intents[e.Key] = e
		}
	}
	for i := range entries {
		e := &entries[i]
		if e.Kind != agentrun.KindOutcome || e.Role != projection.RoleTool || e.Label.Tier < inst.confinedTier {
			continue
		}
		in, ok := intents[e.Key]
		if !ok {
			return "tool call " + e.Name, e.Key
		}
		if window, op, isOp := inst.windowOf(in); isOp {
			return op + " in window " + strconv.FormatUint(window, 10) + " (tool call " + e.Name + ")", e.Key
		}
		return in.Name + " (tool call " + e.Name + ")", e.Key
	}
	return "", ""
}

// exportRefusal is what the export panel says of a confined conversation.
func exportRefusal(confinedBy string) string {
	if confinedBy == "" {
		return "This conversation holds sealed data, which the lading store may not hold: it cannot be exported."
	}
	return "This conversation holds sealed data since " + confinedBy + ", which the lading store may not hold: it cannot be exported."
}
