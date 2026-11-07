package sqlappletcreator

import (
	"hash/fnv"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
)

// The operations catalog (ADR-0269, keelson-app A1–A2): the draft as one
// resource — the four fields and the buffer, named by a revision — a query
// that reads it, a command per field that sets and never toggles, a buffer
// write guarded by the revision as mdedit's propose_edit is, and save, which
// is asynchronous and settles with the store's reply. Export stays off the
// catalog: the Powerbox dialog is the person's own gesture, and a file on
// their disk is not something an agent asks for through a form.

const (
	resDraft = "draft"

	opGetDraft  = "get_draft"
	opSetField  = "set_field"
	opSetBuffer = "set_buffer"
	opSave      = "save"

	fieldSlug    = "slug"
	fieldTitle   = "title"
	fieldSummary = "summary"
	fieldIcon    = "icon"
)

const (
	reasonField    = "field is one of slug, title, summary, icon"
	reasonRevision = "the buffer changed since that revision; read it again with get_draft"
	reasonSaving   = "a save is in flight; its reply settles the call that started it"
)

// DraftValue is the draft resource: the fields and the buffer's revision,
// comparable so the host can tell one state from the next.
type DraftValue struct {
	Slug     string
	Title    string
	Summary  string
	Icon     string
	Revision string `desc:"names the buffer's text; set_buffer quotes it"`
}

// Draft is get_draft's result.
type Draft struct {
	Slug     string `desc:"the store key and the export file's stem; lowercase and dashes"`
	Title    string `desc:"the applet's title; required by the store"`
	Summary  string `desc:"the one line the launcher shows under the title; required by the store"`
	Icon     string `desc:"the applet's icon glyph; optional"`
	Sql      string `desc:"the buffer the applet runs, whole"`
	Revision string `desc:"names this buffer; set_buffer quotes it, and a buffer changed since is refused"`
	Endpoint string `desc:"the endpoint the buffer was authored against: empty or default, or introspection"`
	Saving   bool   `desc:"a save is in flight"`
	Status   string `desc:"the last outcome line the window shows"`
}

// SetFieldArgs is set_field's argument.
type SetFieldArgs struct {
	Field string `desc:"slug, title, summary or icon"`
	Value string `desc:"the field's new text; empty clears it"`
}

// SetBufferArgs is set_buffer's argument.
type SetBufferArgs struct {
	Sql      string `desc:"the buffer's new text"`
	Revision string `desc:"the revision get_draft returned"`
}

// SaveOutcome is what save settles with.
type SaveOutcome struct {
	Saved   bool   `desc:"the store accepted the applet and minted its manifest"`
	Slug    string `desc:"the slug saved under"`
	Class   string `desc:"the class the store assigned"`
	Refused string `desc:"the store's reason when it refused the document"`
	Error   string `desc:"a transport or decode failure; empty otherwise"`
}

// snap is what queries read: a copy taken after the command stage.
type snap struct {
	slug, title, summary, icon, sql, endpoint string
	saving                                    bool
	status                                    string
}

// bufferRevision names a buffer: equal texts, equal revisions.
func bufferRevision(sql string) (rev string) {
	h := fnv.New64a()
	_, _ = h.Write([]byte(sql))
	return strconv.FormatUint(h.Sum64(), 16)
}

func (inst *App) opsSnapshot() (sn snap) {
	inst.mu.Lock()
	sn.saving, sn.status = inst.saveBusy, inst.status
	inst.mu.Unlock()
	sn.slug, sn.title, sn.summary, sn.icon, sn.sql, sn.endpoint = inst.slug, inst.title, inst.summary, inst.icon, inst.sql, inst.endpoint
	return
}

func (inst *App) draftValue() (v DraftValue) {
	v = DraftValue{Slug: inst.slug, Title: inst.title, Summary: inst.summary, Icon: inst.icon, Revision: bufferRevision(inst.sql)}
	return
}

// fieldPtr is the draft field a name addresses.
func (inst *App) fieldPtr(field string) (p *string) {
	switch strings.ToLower(strings.TrimSpace(field)) {
	case fieldSlug:
		return &inst.slug
	case fieldTitle:
		return &inst.title
	case fieldSummary:
		return &inst.summary
	case fieldIcon:
		return &inst.icon
	}
	return nil
}

var ops = func() (s *appops.Set[*App, snap]) {
	s = appops.NewSet((*App).opsSnapshot)
	s.Resource(resDraft, "the applet draft: slug, title, summary, icon and the buffer's revision", func(inst *App) any { return inst.draftValue() })
	s.Editing(resDraft, func(inst *App) bool {
		for _, h := range inst.fieldHandles {
			if appops.WidgetEditing(h) {
				return true
			}
		}
		return false
	})

	appops.Query(s, app.OperationSpec{Name: opGetDraft, Version: 1,
		Summary: "read the draft: the four fields, the buffer and its revision, the endpoint, and whether a save is in flight",
		Reads:   []string{resDraft}, Agents: true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn snap, _ appops.None) (out Draft, err error) {
			out = Draft{Slug: sn.slug, Title: sn.title, Summary: sn.summary, Icon: sn.icon, Sql: sn.sql,
				Revision: bufferRevision(sn.sql), Endpoint: sn.endpoint, Saving: sn.saving, Status: sn.status}
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSetField, Version: 1,
		Summary: "set one of the draft's fields: slug, title, summary or icon",
		Effect:  app.OperationEffectDocument, Writes: []string{resDraft}, Agents: true,
		Gesture: "typing in the field", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in SetFieldArgs) (out appops.None, err error) {
			p := inst.fieldPtr(in.Field)
			if p == nil {
				err = app.RefuseOperation(reasonField)
				return
			}
			*p = in.Value
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSetBuffer, Version: 1,
		Summary: "replace the buffer, quoting the revision get_draft returned; a buffer changed since is refused",
		Effect:  app.OperationEffectDocument, Reads: []string{resDraft}, Writes: []string{resDraft}, Agents: true,
		Gesture: "typing in the editor", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in SetBufferArgs) (out appops.None, err error) {
			if in.Revision != bufferRevision(inst.sql) {
				err = app.RefuseOperation(reasonRevision)
				return
			}
			inst.sql = in.Sql
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSave, Version: 1,
		Summary: "compose the draft and publish it to the applet store, which validates it and mints a manifest; the call stays running until the store replies",
		Effect:  app.OperationEffectConsequential, Reads: []string{resDraft}, Agents: true,
		Gesture: "the Save applet button", Async: true,
		Follows: []string{"the applet joins the Apps menu when the store accepts it; a slug the store holds is overwritten",
			"the store's refusal, in its words, is the settled outcome rather than a failure of the call"}},
		func(inst *App, _ app.OperationCall, _ appops.None) (out SaveOutcome, err error) {
			err = inst.startSave()
			return
		})
	appops.Settles(s, opSave, func(inst *App, _ app.OperationCall) (out SaveOutcome, settled bool, err error) {
		inst.mu.Lock()
		defer inst.mu.Unlock()
		if inst.saveBusy {
			return
		}
		return inst.saveOutcome, true, nil
	})
	s.Available(opSave, func(sn snap) (ok bool, reason string) {
		if sn.saving {
			return false, reasonSaving
		}
		return true, ""
	})
	return
}()

// Operations serves the catalog for this window (app.OperationsAppI).
func (inst *App) Operations() (h app.OperationsHandlerI) { return ops.Bind(inst) }

var _ app.OperationsAppI = (*App)(nil)

// saveGesture is the Save button: through the catalog where a host serves
// it, so the command log sees the click as the person's, and directly
// otherwise (tour, tests).
func (inst *App) saveGesture() {
	if inst.frameCtx != nil {
		if _, err := appops.Gesture[appops.None, SaveOutcome](inst.frameCtx, opSave, appops.None{}); err == nil {
			return
		}
	}
	_ = inst.startSave()
}
