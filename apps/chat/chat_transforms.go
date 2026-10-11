package chat

import (
	"embed"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm/promptbook"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// The transformations (ADR-0318, from ADR-0216 §SD2): requests for the
// markdown editor the person picks in the composer instead of typing them.
// mdedit holds no model grant; the harness reads the document through its
// operations, does the work, and proposes the result there for the
// person's verdict. Each document's body is the request and its scope says
// what it works on; the book is registered with promptbook, so
// keelson('llm_prompts') lists it.

//go:embed transforms
var transformsFS embed.FS

// transformBookId names the book.
const transformBookId = "transforms"

func init() {
	if err := promptbook.Register(transformBookId, help.MustSub(transformsFS, "transforms")); err != nil {
		log.Warn().Err(err).Msg("chat: failed to register the transformation book")
	}
}

// chatTransforms parses the embedded book once. A document that does not
// parse is a programming error the corpus gate test catches.
var chatTransforms = sync.OnceValue(func() (defs []promptbook.PromptDef) {
	defs, errs := promptbook.ParseBook(transformBookId, help.MustSub(transformsFS, "transforms"))
	if len(errs) > 0 {
		panic("chat: the transformation book does not parse: " + errs[0].Error())
	}
	return
})

const (
	transformOnSelection = "Work on the selection in the markdown editor (mdedit), or on the whole document when nothing is selected: read it with mdedit's get_document operation, and offer the result with its propose_edit operation over that span. The person reviews the proposal there before it is applied."
	transformOnDocument  = "Work on the whole document in the markdown editor (mdedit): read it with mdedit's get_document operation, and offer the result with its propose_edit operation over the whole text. The person reviews the proposal there before it is applied."

	tipTransforms = "Fill the message with a request for the markdown editor. The model reads the document through mdedit's operations and proposes the result there; nothing changes until you apply it."
)

// transformRequest is the message a transformation puts in the composer:
// its request, then what it works on and how the result comes back.
func transformRequest(def promptbook.PromptDef) (s string) {
	on := transformOnDocument
	if def.Scope == promptbook.ScopeSelection {
		on = transformOnSelection
	}
	return strings.TrimSpace(def.System) + "\n\n" + on
}

// pickTransform puts a transformation's request in the composer: in place
// of an empty draft, after one the person has started.
func (inst *App) pickTransform(def promptbook.PromptDef) {
	req := transformRequest(def)
	if strings.TrimSpace(inst.draft) == "" {
		inst.draft = req
	} else {
		inst.draft = strings.TrimRight(inst.draft, "\n") + "\n\n" + req
	}
	c.CurrentApplicationState.StateManager.OverrideDatabindingSPtr(&inst.draft)
}

// renderTransformPicker draws the picker above the composer. Only with
// Apps on: without them the model cannot reach the editor.
func (inst *App) renderTransformPicker() {
	defs := chatTransforms()
	if !inst.appsOn() || len(defs) == 0 {
		return
	}
	for range c.HoverText(tipTransforms).KeepIter() {
		for range c.ComboBox(inst.ids.PrepareStr("transform-pick"),
			c.WidgetText().Text("").Keep(),
			c.WidgetText().Text("Edit in mdedit").Keep()).
			KeepIter() {
			for _, def := range defs {
				clicked := false
				for range c.HoverText(def.Summary).KeepIter() {
					clicked = c.Button(inst.ids.PrepareStr("tf-"+def.Slug),
						c.Atoms().Text(transformEntryLabel(def)).Keep()).
						Frame(false).
						SendResp().HasPrimaryClicked()
				}
				if clicked {
					inst.pickTransform(def)
				}
			}
		}
	}
}

// transformEntryLabel is a definition's one-line face: icon and title.
func transformEntryLabel(def promptbook.PromptDef) (s string) {
	if def.Icon == "" {
		return def.Title
	}
	return def.Icon + " " + def.Title
}
