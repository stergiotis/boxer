// Package sqlappletcreator is the standalone SQL-applet authoring window
// (ADR-0132 Update "O4" / ADR-0135 §SD7). It was the "Save applet" menu
// inlined in the SQL Playground's toolbar; factored into its own app, the
// playground opens it over `windowhost.open` with the current buffer, and it
// composes the ADR-0132 §SD1 document. Two outputs (O4 "A+B"): Save publishes
// it on `applet.store.save` — the store service (apps/sqlapplet) validates,
// persists, and mints a live manifest; Export writes it to a user-chosen file
// through the fs Powerbox save dialog (a durable, user-owned artifact). This
// app never imports the playground or the host, only the neutral contracts
// (appletcreatecfg, appletstore, fsbroker).
package sqlappletcreator

import (
	"math"
	"strings"
	"sync"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/apps/sqlappletcreator/appletcreatecfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appletstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker"
	"github.com/stergiotis/boxer/public/observability/eh"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// App is the authoring window. A fresh instance per Open (factory dispatch);
// the launch config seeds the SQL buffer and endpoint at Mount, the user
// fills slug/title/icon, and Save or Export emits. The draft fields are stable
// heap storage the TextEdit bindings write back across frames (the imzero2
// stable-pointer rule).
type App struct {
	ids *c.WidgetIdStack
	bus app.BusI
	log zerolog.Logger

	sql      string // seeded from the launch config; editable
	slug     string
	title    string
	icon     string
	endpoint string // launch-config endpoint id ("" / "default" / "introspection")

	// mu guards the async outcome, delivered off the frame goroutine (both the
	// store round-trip and the Powerbox dialog block, so both run off-thread —
	// the helphost Copy precedent). The two busy flags are independent; status
	// is the shared last-outcome line.
	mu         sync.Mutex
	saveBusy   bool
	exportBusy bool
	status     string
}

var _ app.AppI = (*App)(nil)

func (inst *App) Manifest() app.Manifest { return manifest }

func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.ids = ctx.Ids()
	inst.bus = ctx.Bus()
	inst.log = ctx.Log()
	if raw := ctx.LaunchConfig(); len(raw) > 0 {
		cfg, dErr := buscodec.Decode[appletcreatecfg.AppletCreate](raw)
		if dErr != nil {
			// The host validated the claimed kind at the boundary, so a decode
			// failure here is a real defect (codec drift, corrupt bytes) — a
			// failed mount, never a silently empty buffer (ADR-0135 §SD4).
			err = eh.Errorf("sqlappletcreator: decode launch config: %w", dErr)
			return
		}
		inst.sql = cfg.Sql
		inst.endpoint = cfg.Endpoint
	}
	return
}

func (inst *App) Frame(ctx app.FrameContextI) (err error) {
	inst.renderForm()
	return
}

func (inst *App) Unmount(ctx app.MountContextI) (err error) { return }

// renderForm draws the whole window: the buffer (editable — a plainly-opened
// window can paste its own), the slug/title/icon drafts, the endpoint note,
// the Save / Export actions, and the last outcome line.
func (inst *App) renderForm() {
	ids := inst.ids
	for range c.PanelCentralInside().KeepIter() {
		for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
			for rt := range c.RichTextLabel("Author the buffer below as a SQL applet: give it a slug, title, and optional icon. Save publishes it into the running system (it joins the Apps menu after the store validates and classifies it, ADR-0132 O4); Export writes the document to a file you choose.") {
				rt.Small().Weak()
			}
			c.Separator().Send()

			c.Label("SQL").Send()
			c.TextEdit(ids.PrepareStr("sql"), inst.sql, true).
				CodeEditor().
				DesiredRows(8).
				DesiredWidth(float32(math.Inf(1))).
				HintText("-- the query this applet runs").
				SendRespVal(&inst.sql)

			for range c.Horizontal().KeepIter() {
				c.Label("slug").Send()
				c.TextEdit(ids.PrepareStr("slug"), inst.slug, false).
					HintText("lowercase-dashes").
					SendRespVal(&inst.slug)
			}
			for range c.Horizontal().KeepIter() {
				c.Label("title").Send()
				c.TextEdit(ids.PrepareStr("title"), inst.title, false).
					SendRespVal(&inst.title)
			}
			for range c.Horizontal().KeepIter() {
				c.Label("icon").Send()
				c.TextEdit(ids.PrepareStr("icon"), inst.icon, false).
					SendRespVal(&inst.icon)
			}

			if inst.endpoint == appletcreatecfg.EndpointIntrospection {
				for rt := range c.RichTextLabel("Endpoint: introspection — the applet reopens against the in-process keelson /query endpoint, where its bare keelson('…') resolves.") {
					rt.Small().Weak()
				}
			}

			c.Separator().Send()

			inst.mu.Lock()
			saveBusy := inst.saveBusy
			exportBusy := inst.exportBusy
			status := inst.status
			inst.mu.Unlock()
			// Each action is replaced by a busy label while in flight (the
			// play toolbar pattern — the label holds the slot, no layout jump).
			for range c.Horizontal().KeepIter() {
				if saveBusy {
					c.Label("Saving…").Send()
				} else if c.Button(ids.PrepareStr("save"), c.Atoms().Text("Save applet").Keep()).
					SendResp().HasPrimaryClicked() {
					inst.submit()
				}
				if exportBusy {
					c.Label("Exporting…").Send()
				} else if c.Button(ids.PrepareStr("export"), c.Atoms().Text("Export .md…").Keep()).
					SendResp().HasPrimaryClicked() {
					inst.export()
				}
			}
			if status != "" {
				for rt := range c.RichTextLabel(status) {
					rt.Small().Weak()
				}
			}
		}
	}
}

// composeDoc renders the current drafts into the ADR-0132 §SD1 document and
// returns it with the trimmed slug (used as the store key / export filename
// stem). Validation beyond the trivially-local (title required, empty buffer,
// fence guard) is deliberately the store service's — the one gate the
// committed corpus test also uses. The endpoint is part of the buffer's
// reproduction context (§SD1): a buffer authored against the in-process
// introspection endpoint — where bare keelson('…') is the dialect — must
// reopen there, so only that case stamps the frontmatter.
func (inst *App) composeDoc() (doc []byte, slug string, err error) {
	slug = strings.TrimSpace(inst.slug)
	title := strings.TrimSpace(inst.title)
	icon := strings.TrimSpace(inst.icon)
	endpoint := ""
	if inst.endpoint == appletcreatecfg.EndpointIntrospection {
		endpoint = "introspection"
	}
	doc, err = appletstore.ComposeAppletDoc(title, icon, endpoint, inst.sql)
	return
}

// submit composes the current buffer and publishes it to the applet store
// (O4 "A" — mint into the running system). The store is the moderation gate;
// this app only composes and reports the reply.
func (inst *App) submit() {
	doc, slug, err := inst.composeDoc()
	if err != nil {
		inst.setStatus(err.Error())
		return
	}
	payload, err := appletstore.EncodeSaveRequest(appletstore.SaveRequest{Slug: slug, Doc: doc})
	if err != nil {
		inst.setStatus(err.Error())
		return
	}
	if inst.bus == nil {
		inst.setStatus("no bus wired — cannot reach the applet store")
		return
	}
	inst.mu.Lock()
	inst.saveBusy = true
	inst.status = "saving " + slug + "…"
	inst.mu.Unlock()
	go func() {
		// Clear the busy gate only after the outcome is written (the export()
		// pattern): resetting it before setStatus would briefly re-show the
		// live button while status still read "saving…", inviting a
		// double-fire of the same save.
		defer func() {
			inst.mu.Lock()
			inst.saveBusy = false
			inst.mu.Unlock()
		}()
		replyBytes, reqErr := inst.bus.Request(appletstore.SubjectSave, payload)
		if reqErr != nil {
			inst.log.Warn().Err(reqErr).Str("slug", slug).Msg("sqlappletcreator: save request failed")
			inst.setStatus("save failed: " + reqErr.Error())
			return
		}
		rep, decErr := appletstore.DecodeSaveReply(replyBytes)
		switch {
		case decErr != nil:
			inst.setStatus("save failed: " + decErr.Error())
		case !rep.OK:
			inst.setStatus("refused: " + rep.Error)
		default:
			inst.setStatus("saved " + slug + " (class " + rep.Class + ") — see the Apps menu")
		}
	}()
}

// export composes the current buffer and writes it to a user-chosen file
// through the fs Powerbox save dialog (O4 "B" — a durable, user-owned
// artifact, distinct from the mint). The host renders the picker overlay; the
// broker grants a one-shot write handle for the path the user picks. Both bus
// round-trips block, so the flow runs off the frame goroutine.
func (inst *App) export() {
	doc, slug, err := inst.composeDoc()
	if err != nil {
		inst.setStatus(err.Error())
		return
	}
	if inst.bus == nil {
		inst.setStatus("no bus wired — cannot open the file dialog")
		return
	}
	name := slug
	if name == "" {
		name = "applet"
	}
	reqBytes, err := fsbroker.MarshalDialogRequest(fsbroker.DialogRequest{SuggestedName: name + ".md"})
	if err != nil {
		inst.setStatus(err.Error())
		return
	}
	inst.mu.Lock()
	inst.exportBusy = true
	inst.status = "choose a file…"
	inst.mu.Unlock()
	go func() {
		defer func() {
			inst.mu.Lock()
			inst.exportBusy = false
			inst.mu.Unlock()
		}()
		rawReply, rerr := inst.bus.Request(fsbroker.SubjectDialogWrite, reqBytes)
		if rerr != nil {
			inst.setStatus("file dialog: " + rerr.Error())
			return
		}
		dr, jerr := fsbroker.UnmarshalDialogReply(rawReply)
		if jerr != nil {
			inst.setStatus("dialog reply: " + jerr.Error())
			return
		}
		if !dr.Granted {
			msg := "export cancelled"
			if dr.Reason != "" {
				msg += ": " + dr.Reason
			}
			inst.setStatus(msg)
			return
		}
		// The write-handle op replies a DialogReply acking whether the bytes
		// landed (unlike the read op, which replies the raw body).
		writeReply, werr := inst.bus.Request(dr.HandleSubjectPrefix+".write", doc)
		if werr != nil {
			inst.setStatus("write: " + werr.Error())
			return
		}
		wr, perr := fsbroker.UnmarshalDialogReply(writeReply)
		if perr != nil {
			inst.setStatus("write reply: " + perr.Error())
			return
		}
		if !wr.Granted {
			inst.log.Warn().Str("reason", wr.Reason).Msg("sqlappletcreator: export write refused")
			inst.setStatus("export failed: " + wr.Reason)
			return
		}
		inst.setStatus("exported " + name + ".md")
	}()
}

func (inst *App) setStatus(s string) {
	inst.mu.Lock()
	inst.status = s
	inst.mu.Unlock()
}
