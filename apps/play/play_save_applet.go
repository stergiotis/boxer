package play

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/appletstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/observability/eh"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// play_save_applet.go is the ADR-0132 Update "O4" authoring seam: a "Save
// applet" menu in the full (non-attenuated) top bar that composes the
// current buffer into the standard applet document and submits it on
// `applet.store.save`. The store service is the validation gate; play only
// composes and reports the reply.

// renderSaveAppletMenu draws the menu — slug/title/icon drafts, the Save
// button, and the last outcome line. The request runs off the frame
// goroutine (the helphost Copy precedent); the reply lands in
// saveAppletStatus under its mutex and renders on later frames.
func (inst *PlayApp) renderSaveAppletMenu() {
	ids := inst.ids
	for range c.MenuButton(c.Atoms().Text("Save applet").Keep()).KeepIter() {
		for range c.Horizontal().KeepIter() {
			c.Label("slug").Send()
			c.TextEdit(ids.PrepareStr("saveAppletSlug"), inst.saveAppletSlug, false).
				SendRespVal(&inst.saveAppletSlug)
		}
		for range c.Horizontal().KeepIter() {
			c.Label("title").Send()
			c.TextEdit(ids.PrepareStr("saveAppletTitle"), inst.saveAppletTitle, false).
				SendRespVal(&inst.saveAppletTitle)
		}
		for range c.Horizontal().KeepIter() {
			c.Label("icon").Send()
			c.TextEdit(ids.PrepareStr("saveAppletIcon"), inst.saveAppletIcon, false).
				SendRespVal(&inst.saveAppletIcon)
		}
		if c.Button(ids.PrepareStr("saveAppletGo"), c.Atoms().Text("Save").Keep()).
			SendResp().HasPrimaryClicked() {
			inst.submitSaveApplet()
		}
		inst.saveAppletMu.Lock()
		status := inst.saveAppletStatus
		inst.saveAppletMu.Unlock()
		if status != "" {
			for rt := range c.RichTextLabel(status) {
				rt.Small().Weak()
			}
		}
	}
}

// submitSaveApplet composes and submits the current buffer. Validation
// beyond the trivially-local (empty fields, fence guard) is deliberately
// the store service's — one gate, the same one the committed corpus test
// uses.
func (inst *PlayApp) submitSaveApplet() {
	slug := strings.TrimSpace(inst.saveAppletSlug)
	title := strings.TrimSpace(inst.saveAppletTitle)
	icon := strings.TrimSpace(inst.saveAppletIcon)
	// The endpoint is part of the buffer's reproduction context (ADR-0132
	// §SD1): a buffer authored against the in-process introspection
	// endpoint — where bare keelson('…') is the dialect — must reopen
	// there, not against a server that lacks the macro.
	endpoint := ""
	if ep := introspect.LocalQueryEndpoint(); ep != "" && inst.client != nil && inst.client.URL() == ep {
		endpoint = "introspection"
	}
	doc, err := ComposeAppletDoc(title, icon, endpoint, inst.sql)
	if err != nil {
		inst.setSaveAppletStatus(err.Error())
		return
	}
	payload, err := appletstore.EncodeSaveRequest(appletstore.SaveRequest{Slug: slug, Doc: doc})
	if err != nil {
		inst.setSaveAppletStatus(err.Error())
		return
	}
	bus := inst.bus
	if bus == nil {
		inst.setSaveAppletStatus("no bus wired — cannot reach the applet store")
		return
	}
	inst.setSaveAppletStatus("saving " + slug + "…")
	go func() {
		replyBytes, reqErr := bus.Request(appletstore.SubjectSave, payload)
		if reqErr != nil {
			inst.setSaveAppletStatus("save failed: " + reqErr.Error())
			return
		}
		rep, decErr := appletstore.DecodeSaveReply(replyBytes)
		switch {
		case decErr != nil:
			inst.setSaveAppletStatus("save failed: " + decErr.Error())
		case !rep.OK:
			inst.setSaveAppletStatus("refused: " + rep.Error)
		default:
			inst.setSaveAppletStatus("saved " + slug + " (class " + rep.Class + ") — see the Apps menu")
		}
	}()
}

func (inst *PlayApp) setSaveAppletStatus(s string) {
	inst.saveAppletMu.Lock()
	inst.saveAppletStatus = s
	inst.saveAppletMu.Unlock()
}

// ComposeAppletDoc renders the ADR-0132 §SD1 document for a buffer: the
// documentation-standard frontmatter, a provenance line, and the buffer as
// the single sql fence (O4-D5). endpoint is the frontmatter `endpoint`
// value ("" omits the key — the env-configured default; "introspection"
// pins the in-process endpoint the buffer was authored against). Title is
// required; a buffer containing a fence-opening line is refused rather
// than silently producing a document that re-parses differently.
func ComposeAppletDoc(title string, icon string, endpoint string, sql string) (doc []byte, err error) {
	if strings.TrimSpace(title) == "" {
		err = eh.Errorf("play: applet title is required")
		return
	}
	sql = strings.TrimSpace(sql)
	if sql == "" {
		err = eh.Errorf("play: the buffer is empty")
		return
	}
	for _, line := range strings.Split(sql, "\n") {
		if strings.HasPrefix(line, "```") {
			err = eh.Errorf("play: the buffer contains a fence line (```); it cannot be embedded in an applet document")
			return
		}
	}
	var b strings.Builder
	b.WriteString("---\ntype: reference\naudience: end-user\nstatus: draft\ntitle: ")
	b.WriteString(strconv.Quote(title))
	b.WriteString("\n")
	if icon != "" {
		b.WriteString("icon: ")
		b.WriteString(strconv.Quote(icon))
		b.WriteString("\n")
	}
	if endpoint != "" {
		b.WriteString("endpoint: ")
		b.WriteString(strconv.Quote(endpoint))
		b.WriteString("\n")
	}
	b.WriteString("---\n\n# ")
	b.WriteString(title)
	b.WriteString("\n\nSaved from the SQL Playground (ADR-0132 O4).\n\n```sql\n")
	b.WriteString(sql)
	b.WriteString("\n```\n")
	doc = []byte(b.String())
	return
}
