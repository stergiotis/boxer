package widgets

import (
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/decision"
)

func init() {
	registry.Register(registry.Demo{
		Name:     "decision",
		Category: "Layout & widgets",
		Title:    icons.PhQuestion + " decision",
		Stage:    [2]float32{1200, 1000},
		Flags:    registry.DemoFlagNeedsLargeArea,
		Kind:     registry.DemoKindMixed,
		Description: "widgets/decision (ADR-0404 SD3, SD4): a request and a confirmation, where the affirmative is " +
			"primary; a destructive one; a stale question with a disabled affirmative and its reason; " +
			"one with a form; one with a long detail after its options; and a request in a column 300 points wide, " +
			"where what follows wraps and a note of caution takes the fill off the affirmative.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			state = &decisionDemoState{}
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoDecision(ids, state.(*decisionDemoState))
		},
		SourceFunc: demoDecision,
	})
}

// decisionDemoState is what the gallery window remembers: the last option
// chosen and the form's input.
type decisionDemoState struct {
	last    string
	addNote bool
}

func demoDecision(ids *c.WidgetIdStack, st *decisionDemoState) {
	last := "nothing chosen yet"
	if st.last != "" {
		last = "last chosen: " + st.last
	}
	c.Label(last).Send()
	pick := func(scope string, res decision.Result) {
		if res.Has {
			st.last = scope + " / " + res.Chosen
		}
	}
	for range c.HorizontalTop().KeepIter() {
		for range c.Vertical().KeepIter() {
			c.UiSetMaxWidth(420)
			pick("request", decision.Render(decision.Input{
				Ids: ids, ScopeKey: "request", Origin: decision.OriginRequest,
				Title:   "The assistant asks to start a task.",
				Subject: "Read the notes window and suggest edits",
				Follows: []string{"It may read the notes window until the task ends. Nothing it suggests is applied without you."},
				Options: []decision.Option{
					{Key: "approve", Label: "Approve", Role: decision.RoleAffirm},
					{Key: "decline", Label: "Decline", Role: decision.RoleDecline},
				},
			}))
			pick("confirm", decision.Render(decision.Input{
				Ids: ids, ScopeKey: "confirm", Origin: decision.OriginConfirm,
				Title:   "Export the artefact to the shared store?",
				Subject: "revision 4 · 2 files · 18 KB",
				Follows: []string{"Kept for 30 days. Anyone who can read the store can read these files until then."},
				Options: []decision.Option{
					{Key: "export", Label: "Export", Role: decision.RoleAffirm, Tooltip: "writes one snapshot"},
					{Key: "cancel", Label: "Cancel", Role: decision.RoleDecline},
				},
			}))
			pick("destroy", decision.Render(decision.Input{
				Ids: ids, ScopeKey: "destroy", Origin: decision.OriginConfirm,
				Title:   "Delete the conversation?",
				Subject: "Weather station survey",
				Follows: []string{"Its transcript and artefact are removed from this machine. The journal keeps that it existed."},
				Options: []decision.Option{
					{Key: "delete", Label: "Delete", Role: decision.RoleAffirm, Destroys: true},
					{Key: "keep", Label: "Keep", Role: decision.RoleDecline},
				},
			}))
			pick("stale", decision.Render(decision.Input{
				Ids: ids, ScopeKey: "stale", Origin: decision.OriginRequest,
				Title:   "The model proposes a change to the artefact.",
				Follows: []string{"Accepting writes revision 5 over what you see now."},
				Notes:   []decision.Note{{Text: "The artefact moved since the model proposed this.", Tone: styletokens.ToneWarning}},
				Options: []decision.Option{
					{Key: "accept", Label: "Accept", Role: decision.RoleAffirm, Disabled: true,
						Why: "Accept is off: the proposal was made against revision 3. Ask the model to propose it again."},
					{Key: "reject", Label: "Reject", Role: decision.RoleDecline},
					{Key: "later", Label: "Later", Role: decision.RoleOther},
				},
			}))
		}
		c.AddSpace(styletokens.GapSections(styletokens.ActiveDensity()))
		for range c.Vertical().KeepIter() {
			c.UiSetMaxWidth(420)
			pick("form", decision.Render(decision.Input{
				Ids: ids, ScopeKey: "form", Origin: decision.OriginConfirm,
				Title:   "Save the screenshot into the artefact?",
				Follows: []string{"The image is stored with the next revision and shown to the model when it reads the artefact."},
				Form: func() {
					c.Checkbox(ids.PrepareStr("note"), st.addNote, "add a caption under the image").SendRespVal(&st.addNote)
				},
				Options: []decision.Option{
					{Key: "save", Label: "Save", Role: decision.RoleAffirm},
					{Key: "discard", Label: "Discard", Role: decision.RoleDecline},
				},
			}))
			pick("detail", decision.Render(decision.Input{
				Ids: ids, ScopeKey: "detail", Origin: decision.OriginRequest,
				Title:   "The model proposes a change to the artefact.",
				Subject: "revision 3 → 4 · 24 lines",
				Follows: []string{"Accepting writes revision 4; the diff below is what changes."},
				Options: []decision.Option{
					{Key: "accept", Label: "Accept", Role: decision.RoleAffirm},
					{Key: "reject", Label: "Reject", Role: decision.RoleDecline},
				},
				Detail: func() {
					for i := range 24 {
						for rt := range c.RichTextLabel("+ line " + strconv.Itoa(i+1) + " of the proposed section") {
							rt.Monospace()
						}
					}
				},
			}))
		}
		c.AddSpace(styletokens.GapSections(styletokens.ActiveDensity()))
		for range c.Vertical().KeepIter() {
			c.UiSetMaxWidth(300)
			c.Label("In a column 300 points wide:").Send()
			pick("narrow", decision.Render(decision.Input{
				Ids: ids, ScopeKey: "narrow", Origin: decision.OriginRequest,
				Title:   "The assistant asks to export the artefact to the shared store.",
				Subject: "revision 4 · 2 files",
				Follows: []string{
					"Kept for 30 days. Anyone who can read the shared store can read these files until then.",
					"No endorsement covers this revision: confirming also endorses it for publishing.",
				},
				Notes: []decision.Note{{Text: "This conversation has read untrusted content: what the model writes may have been steered by it.", Tone: styletokens.ToneWarning}},
				Options: []decision.Option{
					{Key: "export", Label: "Export and endorse", Role: decision.RoleAffirm},
					{Key: "decline", Label: "Decline", Role: decision.RoleDecline},
					{Key: "later", Label: "Ask me later", Role: decision.RoleOther},
				},
			}))
		}
	}
}
