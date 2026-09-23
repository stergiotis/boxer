package play

// play_model_panel.go — the Model tab (ADR-0254 §SD6): play's model
// affordance as a transformation book rather than a bespoke ask panel. Three
// prompt documents over the buffer — explain, fix this error, ask — one
// bgjob-backed flow with a preview, and Insert / Replace over the editor
// delivery ops. Generated SQL never executes unseen: every gesture stops at
// the preview, and Replace is a click made after the SQL is on screen.
//
// What survives of the withdrawn ADR-0120 holds here unchanged: generation
// is compile-only through the text2sql2 orchestrator (its SD1), with the
// editor-delivery ops carrying the result; grounding is the schema tier
// the semantic layer of ADR-0139 owns — the T0 harvest of the pinned
// endpoint's `system.columns` until the layer exists; and the target is
// the nanopass canonical dialect (SD7), which the orchestrator's validation
// loop enforces. An `explain` is a plain completion rendered as markdown; a
// `fix` and an `ask` go through the orchestrator, whose repair loop is what
// keeps the answer valid.
//
// The model is the host's (ADR-0254 §SD2): the tab asks `llm.describe`
// once, off the frame, and renders only when the host offers one. The
// request forwards the sensitivity of what it was composed from (§SD3):
// the buffer's own dispatch label, since a buffer naming a sealed dataset
// is what would carry confined content.

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/db/clickhouse/text2sql2/observers"
	"github.com/stergiotis/boxer/public/db/clickhouse/text2sql2/orchestrator"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm/promptbook"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/llm/openaichat"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/jobprogress"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

const (
	tipModelPicker = "Pick a prompt. Each is a document in play's prompt book; its tooltip says what it does and what it runs over."
	tipModelRun    = "Run the picked prompt over the editor buffer (or the question). The result lands below for review — nothing is inserted or run until you say so."
	tipModelHost   = "Where the text is sent: the host's model endpoint, from BOXER_LLM_ENDPOINT."
	tipModelInsert = "Insert the generated SQL at the editor caret."
	tipModelSwap   = "Replace the editor buffer with the generated SQL. It does not run until you press Run."
	tipModelCopy   = "Copy the result to the clipboard."
	tipModelDrop   = "Drop the result."
	tipModelTrunc  = "The completion hit the token ceiling — this is everything the model produced. Raise BOXER_LLM_MAXTOKENS (or the prompt's max-tokens) for more room."

	// modelSchemaByteCap bounds the harvested schema text a prompt carries:
	// the evidence says a few KB of grounding is what moves accuracy, and a
	// wide database must not turn one ask into a hundred-KB prompt.
	modelSchemaByteCap = 24 << 10
	// modelSchemaRowCap bounds the harvest itself.
	modelSchemaRowCap = 4000
	// modelAttempts is the orchestrator's repair budget per ask.
	modelAttempts = 3
)

var (
	atomsModelRun    = c.Atoms().Text("Run").Keep()
	atomsModelInsert = c.Atoms().Text("Insert").Keep()
	atomsModelSwap   = c.Atoms().Text("Replace").Keep()
	atomsModelCopy   = c.Atoms().Text("Copy").Keep()
	atomsModelDrop   = c.Atoms().Text("Discard").Keep()
)

// modelResult is one finished run: markdown prose from an explain, or SQL
// from a fix or an ask, plus the provenance the pane header shows.
type modelResult struct {
	// Content is the answer. SQL is true when it is a statement for the
	// editor rather than prose for the reader.
	Content string
	SQL     bool
	Elapsed time.Duration
	// Attempts is the orchestrator's count for an ask or a fix; 1 for an
	// explain.
	Attempts     int
	InputTokens  int32
	OutputTokens int32
	Truncated    bool
}

// modelState is everything the tab owns, one field on PlayApp.
type modelState struct {
	// cli is the host's model over the bus; enabled, model and host land
	// when the describe started on the first frame comes back (avail).
	cli       *llm.Client
	avail     bgjob.Runner[llm.Description]
	asked     bool
	enabled   bool
	model     string
	host      string
	unavail   string
	defs      []promptbook.PromptDef
	defsOk    bool
	sel       int
	question  string
	lastErr   string
	runner    bgjob.Runner[modelResult]
	reqTitle  string
	res       *modelResult
	resDoc    *markdown.Doc
	resDocSrc string
	errText   string
}

// renderModelTab is the tab body. It asks the host once, then renders the
// picker, the run and the result.
func (inst *PlayApp) renderModelTab(f *TabFrame) {
	m := &inst.model
	if f != nil && f.Err != nil {
		m.lastErr = f.Err.Error()
	} else if f != nil && !f.Loading && f.Err == nil && !f.Executed.IsZero() {
		m.lastErr = ""
	}
	inst.startModelDescribe()
	inst.drainModel()
	switch {
	case !m.asked:
		c.Label("This window has no bus to reach the host's model through.").Send()
		return
	case !m.enabled && m.unavail != "":
		c.Label(m.unavail).Send()
		return
	case !m.enabled:
		c.Label("Asking the host whether it offers a model…").Send()
		c.RequestRepaint()
		return
	}
	inst.renderModelPicker()
	inst.renderModelProgress()
	if m.res != nil || m.errText != "" {
		c.AddSpace(styletokens.GapSections(styletokens.DensityStandard))
		inst.renderModelResult()
	}
}

// startModelDescribe asks the host once, off the frame, whether it offers
// a model.
func (inst *PlayApp) startModelDescribe() {
	m := &inst.model
	if m.asked || inst.bus == nil {
		return
	}
	m.asked = true
	cli := llm.NewClient(inst.bus)
	m.cli = cli
	m.avail.Start(nil, bgjob.Spec{Kind: "play-model-describe", Title: "model"},
		func(ctx context.Context) (d *llm.Description, err error) {
			got, err := cli.Describe(ctx)
			if err != nil {
				return
			}
			d = &got
			return
		})
}

// drainModel lands the describe and the run on the render thread, once per
// frame.
func (inst *PlayApp) drainModel() {
	m := &inst.model
	if d, _, ok := m.avail.TakeResult(); ok {
		m.enabled = d.Configured
		m.model, m.host = d.Model, d.EndpointHost
		if !d.Configured {
			m.unavail = "The host offers no model: " + d.Reason + "."
		}
	} else if snap := m.avail.Snapshot(); snap.State == bgjob.StateFailed {
		m.avail.Invalidate()
		m.unavail = "The host could not be asked for a model: " + promptbook.FailureLine(snap.Err) + "."
	}
	if res, _, ok := m.runner.TakeResult(); ok {
		m.res, m.errText = res, ""
		m.resDoc, m.resDocSrc = nil, ""
		return
	}
	snap := m.runner.Snapshot()
	if snap.State != bgjob.StateFailed {
		return
	}
	m.runner.Invalidate()
	if errors.Is(snap.Err, context.Canceled) {
		return
	}
	m.res = nil
	m.errText = promptbook.FailureLine(snap.Err)
	if snap.Err != nil {
		m.errText += " — " + snap.Err.Error()
	}
}

// ensureModelDefs loads the book once.
func (inst *PlayApp) ensureModelDefs() {
	m := &inst.model
	if m.defsOk {
		return
	}
	m.defsOk = true
	defs, errs := promptbook.Book(modelBookId)
	for _, e := range errs {
		inst.logger.Warn().Err(e).Msg("play: model prompt failed to parse")
	}
	m.defs = defs
}

func (inst *PlayApp) renderModelPicker() {
	m := &inst.model
	inst.ensureModelDefs()
	if len(m.defs) == 0 {
		c.Label("The prompt book is empty.").Send()
		return
	}
	if m.sel < 0 || m.sel >= len(m.defs) {
		m.sel = 0
	}
	cur := m.defs[m.sel]

	for range c.HorizontalTop().KeepIter() {
		for range c.HoverText(tipModelPicker).KeepIter() {
			for range c.ComboBox(inst.ids.PrepareStr("model-pick"),
				c.WidgetText().Text("Prompt").Keep(),
				c.WidgetText().Text(modelEntryLabel(cur)).Keep()).KeepIter() {
				for i, def := range m.defs {
					clicked := false
					for range c.HoverText(def.Summary).KeepIter() {
						clicked = c.Button(inst.ids.PrepareStr("model-def-"+def.Slug),
							c.Atoms().Text(modelEntryLabel(def)).Keep()).
							Frame(false).Selected(i == m.sel).SendResp().HasPrimaryClicked()
					}
					if clicked {
						m.sel = i
					}
				}
			}
		}
		run := false
		for range c.HoverText(tipModelRun).KeepIter() {
			run = c.Button(inst.ids.PrepareStr("model-run"), atomsModelRun).SendResp().HasPrimaryClicked()
		}
		if run && !m.runner.Running() {
			inst.startModelRun(cur)
		}
		for range c.HoverText(tipModelHost).KeepIter() {
			c.Label("→ " + m.model + " · " + m.host).Selectable(false).Send()
		}
	}
	switch cur.Scope {
	case promptbook.ScopeQuestion:
		c.TextEdit(inst.ids.PrepareStr("model-question"), m.question, true).SendRespVal(&m.question)
	case promptbook.ScopeBufferAndError:
		if m.lastErr == "" {
			c.Label("The last run had no error; the prompt runs over the buffer alone.").Selectable(false).Send()
		}
	}
}

// modelEntryLabel is a definition's one-line face: icon and title.
func modelEntryLabel(def promptbook.PromptDef) (s string) {
	if def.Icon == "" {
		return def.Title
	}
	return def.Icon + " " + def.Title
}

// startModelRun snapshots what the prompt runs over and starts the job.
// Everything the compute closure needs is copied here on the render
// thread — the bgjob contract.
func (inst *PlayApp) startModelRun(def promptbook.PromptDef) {
	m := &inst.model
	buffer, question, lastErr := inst.sql, strings.TrimSpace(m.question), m.lastErr
	if def.Scope == promptbook.ScopeQuestion && question == "" {
		m.errText = "Type a question first."
		return
	}
	if def.Scope != promptbook.ScopeQuestion && strings.TrimSpace(buffer) == "" {
		m.errText = "The editor is empty."
		return
	}
	m.reqTitle = def.Title
	m.res, m.errText = nil, ""
	m.resDoc, m.resDocSrc = nil, ""
	cli, client := m.cli, inst.client
	ok := m.runner.StartReporting(nil,
		bgjob.Spec{Kind: "play-model", Title: def.Title},
		func(ctx context.Context, _ bgjob.Reporter) (res *modelResult, err error) {
			var r modelResult
			r, err = runModelPrompt(ctx, cli, client, def, buffer, question, lastErr)
			if err != nil {
				return
			}
			res = &r
			return
		})
	if !ok {
		m.errText = "a prompt is already running"
	}
}

// runModelPrompt is the compute half, off the render thread: an explain is
// one completion; a fix or an ask compiles through the orchestrator.
func runModelPrompt(ctx context.Context, cli *llm.Client, client *Client, def promptbook.PromptDef, buffer, question, lastErr string) (res modelResult, err error) {
	started := time.Now()
	sensitivity := queryengine.SensitivityOrdinary
	if client != nil && strings.TrimSpace(buffer) != "" {
		// The buffer's own placement label: a statement naming a sealed
		// dataset is what would carry confined content (ADR-0254 §SD3).
		sensitivity = client.Dispatch(buffer, "").sensitivity
	}
	switch def.Scope {
	case promptbook.ScopeBuffer:
		var r promptbook.Result
		r, err = promptbook.Run(ctx, modelCompleter{cli: cli, sensitivity: sensitivity}, def, "```sql\n"+buffer+"\n```")
		if err != nil {
			return
		}
		res = modelResult{Content: r.Content, Elapsed: r.Elapsed, Attempts: 1, InputTokens: r.InputTokens, OutputTokens: r.OutputTokens, Truncated: r.Truncated}
		return
	case promptbook.ScopeBufferAndError, promptbook.ScopeQuestion:
		var schema string
		if client != nil {
			schema, err = harvestSchema(ctx, client)
			if err != nil {
				return res, eh.Errorf("model: schema harvest: %w", err)
			}
		}
		input := question
		if def.Scope == promptbook.ScopeBufferAndError {
			input = fixInput(buffer, lastErr)
		}
		orch := orchestrator.New(orchestrator.Config{
			DefaultModel: "host", MaxAttempts: modelAttempts,
			Schema: schema + "\n" + def.System,
		}, modelChat{cli: cli, purpose: def.Purpose(), sensitivity: sensitivity, temperature: def.Temperature, maxTokens: def.MaxTokens},
			nil, modelNoCache{}, observers.NopObserver{})
		out, cerr := orch.Compile(ctx, input)
		if cerr != nil {
			return res, cerr
		}
		res = modelResult{Content: out.SQL, SQL: true, Elapsed: time.Since(started), Attempts: out.Attempts}
		return
	default:
		return res, eb.Build().Str("slug", def.Slug).Errorf("model: the prompt's scope is not one this pane runs")
	}
}

// fixInput is what a fix prompt reads: the statement and the error it
// produced.
func fixInput(buffer, lastErr string) (s string) {
	var b strings.Builder
	b.WriteString("This ClickHouse query fails.\n\n```sql\n")
	b.WriteString(strings.TrimSpace(buffer))
	b.WriteString("\n```\n\n")
	if strings.TrimSpace(lastErr) != "" {
		b.WriteString("The error:\n\n")
		b.WriteString(strings.TrimSpace(lastErr))
		b.WriteString("\n\n")
	}
	b.WriteString("Return the corrected query.")
	return b.String()
}

// modelCompleter forwards the buffer's sensitivity on a plain completion.
type modelCompleter struct {
	cli         *llm.Client
	sensitivity queryengine.SensitivityE
}

func (inst modelCompleter) Complete(ctx context.Context, req llm.Request) (res llm.Response, err error) {
	req.Sensitivity = inst.sensitivity
	return inst.cli.Complete(ctx, req)
}

// modelChat is the orchestrator's client seam over the host's model
// (ADR-0254 §SD6): the model named by the orchestrator is ignored, since
// the host owns it. A truncated completion is handed over as its content;
// the orchestrator's validation says whether it was enough.
type modelChat struct {
	cli         *llm.Client
	purpose     string
	sensitivity queryengine.SensitivityE
	temperature *float32
	maxTokens   int32
}

func (inst modelChat) Chat(ctx context.Context, _ string, messages []orchestrator.Message) (response string, err error) {
	req := llm.Request{Purpose: inst.purpose, Sensitivity: inst.sensitivity, Temperature: inst.temperature, MaxTokens: inst.maxTokens}
	for _, msg := range messages {
		om, ok := chatMessage(msg)
		if !ok {
			return "", eb.Build().Str("role", msg.Role).Errorf("model: unknown chat role")
		}
		req.Messages = append(req.Messages, om)
	}
	res, err := inst.cli.Complete(ctx, req)
	if err != nil {
		return
	}
	response = res.Content
	return
}

// chatMessage maps the orchestrator's stringly-typed role onto the client's.
// An unknown role is refused rather than defaulted: a message sent under the
// wrong role is a prompt injection the reader never sees.
func chatMessage(msg orchestrator.Message) (out openaichat.Message, ok bool) {
	switch msg.Role {
	case "system":
		return openaichat.Message{Role: openaichat.ChatRoleSystem, Content: msg.Content}, true
	case "user":
		return openaichat.Message{Role: openaichat.ChatRoleUser, Content: msg.Content}, true
	case "assistant":
		return openaichat.Message{Role: openaichat.ChatRoleAssistant, Content: msg.Content}, true
	}
	return
}

// modelNoCache: an ask is compiled every time — a cached compilation would
// hide a changed schema or a changed prompt behind an old answer.
type modelNoCache struct{}

func (modelNoCache) Get(string) (entry *orchestrator.CacheEntry, found bool) { return }
func (modelNoCache) Put(string, *orchestrator.CacheEntry)                    {}

// harvestSchema is the T0 grounding tier (ADR-0139 §SD6): the pinned
// endpoint's current database as `table: column Type -- comment` lines,
// capped so a wide database does not turn one ask into a huge prompt.
func harvestSchema(ctx context.Context, client *Client) (text string, err error) {
	q := "SELECT database, table, name, type, comment FROM system.columns " +
		"WHERE database = currentDatabase() ORDER BY database, table, position LIMIT " + itoa(modelSchemaRowCap) + " FORMAT TabSeparated"
	raw, err := client.queryTabSeparated(ctx, q, nil)
	if err != nil {
		return
	}
	text = formatSchema(string(raw), modelSchemaByteCap)
	return
}

// formatSchema turns the harvest's TabSeparated rows into the prompt's
// schema block: one heading per table, one line per column.
func formatSchema(tsv string, byteCap int) (text string) {
	var b strings.Builder
	lastTable := ""
	for line := range strings.SplitSeq(tsv, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 4 {
			continue
		}
		table := f[0] + "." + f[1]
		if table != lastTable {
			if lastTable != "" {
				b.WriteByte('\n')
			}
			b.WriteString(table)
			b.WriteString(":\n")
			lastTable = table
		}
		b.WriteString("  ")
		b.WriteString(f[2])
		b.WriteByte(' ')
		b.WriteString(f[3])
		if len(f) > 4 && strings.TrimSpace(f[4]) != "" {
			b.WriteString(" -- ")
			b.WriteString(strings.ReplaceAll(f[4], `\t`, " "))
		}
		b.WriteByte('\n')
		if b.Len() > byteCap {
			b.WriteString("  … (schema truncated)\n")
			break
		}
	}
	return b.String()
}

// renderModelProgress draws the transient progress block while a run is in
// flight, with the cancel the timeout story leans on.
func (inst *PlayApp) renderModelProgress() {
	m := &inst.model
	if !m.runner.Running() {
		return
	}
	c.RequestRepaint()
	snap := m.runner.Snapshot()
	if jobprogress.Render(jobprogress.Input{
		Title: m.reqTitle, Fraction: snap.Fraction, EtaMs: snap.EtaMs, Note: snap.Note,
		CancelId: inst.ids.PrepareStr("model-cancel"),
	}) {
		m.runner.Cancel()
	}
}

// renderModelResult draws the verdict block: what ran, the provenance, the
// verdict buttons, then the result — markdown for prose, the statement for
// SQL. Insert and Replace are the editor-delivery ops; nothing runs.
func (inst *PlayApp) renderModelResult() {
	m := &inst.model
	for range c.HorizontalTop().KeepIter() {
		c.Label(m.reqTitle).Selectable(false).Send()
		if m.res != nil {
			c.Label(inst.modelSummaryLine()).Selectable(false).Send()
			if m.res.Truncated {
				badge.New(inst.ids.PrepareStr("model-trunc"), "truncated").
					Tone(badge.ToneWarning).Variant(badge.VariantSoft).Size(badge.SizeSm).Tooltip(tipModelTrunc).Send()
			}
		}
		insert, swap, copyRes, drop := false, false, false, false
		if m.res != nil && m.res.SQL {
			for range c.HoverText(tipModelInsert).KeepIter() {
				insert = c.Button(inst.ids.PrepareStr("model-insert"), atomsModelInsert).SendResp().HasPrimaryClicked()
			}
			for range c.HoverText(tipModelSwap).KeepIter() {
				swap = c.Button(inst.ids.PrepareStr("model-swap"), atomsModelSwap).SendResp().HasPrimaryClicked()
			}
		}
		if m.res != nil && inst.CanCopy() {
			for range c.HoverText(tipModelCopy).KeepIter() {
				copyRes = c.Button(inst.ids.PrepareStr("model-copy"), atomsModelCopy).SendResp().HasPrimaryClicked()
			}
		}
		for range c.HoverText(tipModelDrop).KeepIter() {
			drop = c.Button(inst.ids.PrepareStr("model-drop"), atomsModelDrop).SendResp().HasPrimaryClicked()
		}
		switch {
		case insert:
			inst.InsertSqlAtCaret(m.res.Content)
		case swap:
			inst.ReplaceSql(m.res.Content)
		case copyRes:
			inst.copyToClipboard(m.res.Content)
		case drop:
			m.res, m.errText = nil, ""
			m.resDoc, m.resDocSrc = nil, ""
		}
	}
	if m.errText != "" {
		badge.New(inst.ids.PrepareStr("model-err"), m.errText).Tone(badge.ToneError).Variant(badge.VariantSoft).Send()
		return
	}
	if m.res == nil {
		return
	}
	if m.res.SQL {
		c.Label(m.res.Content).Send()
		return
	}
	if m.resDoc == nil || m.resDocSrc != m.res.Content {
		m.resDoc = markdown.Parse([]byte(m.res.Content))
		m.resDocSrc = m.res.Content
	}
	for range c.IdScope(inst.ids.PrepareStr("model-doc")) {
		m.resDoc.Render(inst.ids)
	}
}

// modelSummaryLine is the pane header's provenance readout.
func (inst *PlayApp) modelSummaryLine() (s string) {
	m := &inst.model
	var b strings.Builder
	b.WriteString(m.model)
	b.WriteString(" · ")
	b.WriteString(m.host)
	b.WriteString(" · ")
	b.WriteString(m.res.Elapsed.Round(100 * time.Millisecond).String())
	if m.res.Attempts > 1 {
		b.WriteString(" · ")
		b.WriteString(itoa(m.res.Attempts))
		b.WriteString(" attempts")
	}
	if m.res.InputTokens > 0 || m.res.OutputTokens > 0 {
		b.WriteString(" · ")
		b.WriteString(itoa(int(m.res.InputTokens)))
		b.WriteString("→")
		b.WriteString(itoa(int(m.res.OutputTokens)))
		b.WriteString(" tokens")
	}
	return b.String()
}
