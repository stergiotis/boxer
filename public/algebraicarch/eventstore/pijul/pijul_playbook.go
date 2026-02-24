//go:build llm_generated_gemini3pro

package pijul

import (
	"fmt"

	c "github.com/stergiotis/pebble2impl/src/go/public/thestack/imzero2/egui2/components"
)

type Playbook struct {
	Title string
	Steps []string
}

var AllPlaybooks = []Playbook{
	{
		Title: "Commutative Field Merging & Conflict Trapping",
		Steps: []string{
			"Alice edits /contact/email and clicks [Save].",
			"Bob edits /company/name and clicks [Save].",
			"Alice clicks [Push] to origin.",
			"Bob clicks [Pull] from origin.",
			"Verify: Bob sees Alice's email change without overwriting his company name (Commutative patch).",
			"Both edit /account/status to different values. Both push/pull.",
			"Verify: UI natively parses the injected file markers.",
			"Click [Keep Alice] or [Keep Bob] to safely resolve the conflict.",
		},
	},
	{
		Title: "Decentralized USB/Email Sync",
		Steps: []string{
			"Bob modifies /company/name -> [Save].",
			"Bob clicks [Email Patch].",
			"Verify: Patch appears in the center column Inbox.",
			"Alice clicks [Apply to Alice].",
			"Verify: Alice receives the update strictly peer-to-peer.",
			"Charlie edits Bob's exact line, clicks [Save] and [Email Patch].",
			"Alice clicks [Apply to Alice] on Charlie's patch BEFORE Bob's.",
			"Verify: Pijul captures structural dependency error and blocks the merge.",
		},
	},
	{
		Title: "The Surgical Revert (Data Correction)",
		Steps: []string{
			"Bob edits /company/name to 'Acme Crap' -> [Save] & [Push].",
			"Alice edits /contact/email -> [Save] & [Push].",
			"Bob realizes his mistake and copies his bad hash from the provenance label.",
			"Bob executes 'pijul unrecord <hash>' via CLI and pushes.",
			"Verify: Company name reverts, but Alice's email remains intact (Surgical Revert).",
		},
	},
	{
		Title: "Data Stewardship & Approval Workflows (Channels)",
		Steps: []string{
			"Charlie creates a new channel: 'pijul channel new charlie-edits'.",
			"Charlie mass-updates 3 fields and pushes his channel.",
			"Alice (on main channel) reviews Charlie's channel.",
			"Alice pulls and merges Charlie's specific changes.",
			"Verify: The Server's main state updates cleanly (Data Stewardship).",
		},
	},
	{
		Title: "The \"Rogue Script\" & Cherry-Picking",
		Steps: []string{
			"Bob edits /contact/phone -> [Save], [Email Patch].",
			"Bob edits /account/status to 'Suspended' (Bug) -> [Save], [Email Patch].",
			"Alice sees both patches in her Inbox.",
			"Alice applies ONLY the phone patch, ignoring the status patch.",
			"Verify: Alice gets the phone update without the bug (Cherry-Picking).",
		},
	},
}

func renderPlaybook(store *DemoStore, ids *c.WidgetIdStack) {
	for i, playbook := range AllPlaybooks {
		for range c.CollapsingHeader(ids.PrepareSeq(uint64(i+1)), c.WidgetText().Text(playbook.Title).Keep()).KeepIter() {
			for j, step := range playbook.Steps {
				// 1. Get or create a STABLE boolean pointer
				key := fmt.Sprintf("%d_%d", i, j)
				valPtr, ok := store.ChecklistState[key]
				if !ok {
					v := false
					valPtr = &v
					store.ChecklistState[key] = valPtr
				}

				// 2. Render Checkbox and bind the stable pointer
				c.Checkbox(ids.PrepareStr(key), *valPtr, fmt.Sprintf("%d. %s", j+1, step)).SendRespVal(valPtr)
			}
		}
	}
}
