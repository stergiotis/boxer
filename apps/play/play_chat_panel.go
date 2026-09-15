package play

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/dustin/go-humanize"
	"github.com/stergiotis/boxer/public/hmi/gloss"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/chatview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// play_chat_panel.go is the ADR-0239 Chat dock tab: a result set rendered as
// a message transcript. The panel observes the active result on chMain, like
// Kanban, and optionally reads a roster on chParticipants and one row per
// reaction on chReactions — each a CTE of the same query, demanded on its own
// lane the way the board's `lanes` CTE is (ADR-0122 §SD6).
//
// The contract is named columns rather than detection (§SD1): `ts`, `sender`
// and `body` are required, the rest optional, and a name is matched on its
// gloss label so `body@text/markdown` still claims `body`. Every column the
// contract does not claim renders inside the bubble through its gloss —
// which is how an image or a JSON payload reaches a message without the
// pane knowing either.

const (
	chatTsCol           = "ts"
	chatSenderCol       = "sender"
	chatBodyCol         = "body"
	chatIdCol           = "id"
	chatReplyToCol      = "reply_to"
	chatSystemCol       = "system"
	chatDeletedCol      = "deleted"
	chatEditedAtCol     = "edited_at"
	chatStatusCol       = "status"
	chatConversationCol = "conversation"
	// The roster's and the reactions' columns (§SD1).
	chatNameCol  = "name"
	chatColorCol = "color"
	chatKeyCol   = "key"
	// chatParticipantsNodeID / chatReactionsNodeID are the CTEs the two
	// optional channels read: nodes of the user's own split graph, so a
	// transcript and its reactions are one query rather than two buffers to
	// keep in sync.
	chatParticipantsNodeID NodeID = "participants"
	chatReactionsNodeID    NodeID = "reactions"
	// chatMaxMessages bounds the fold. A transcript is read from its tail,
	// so the newest rows are kept and the excess is counted in the status
	// line rather than silently dropped.
	chatMaxMessages = 5000
	// chatNobody is the viewer picker's first entry.
	chatNobody = "nobody"
)

// chatClaim is the panel's main-channel claim: the resolved columns (-1 when
// an optional one is absent), the time units, the unclaimed columns in
// schema order, and the selected row from the signal.
type chatClaim struct {
	tsCol, senderCol, bodyCol              int
	idCol, replyCol, systemCol, deletedCol int
	editedCol, statusCol, convCol          int
	tsUnit, editedUnit                     arrow.TimeUnit
	extraCols                              []int
	selRow                                 int64
}

// chatRosterClaim is the participants channel's claim.
type chatRosterClaim struct {
	keyCol, nameCol, colorCol int
}

// chatReactionsClaim is the reactions channel's claim.
type chatReactionsClaim struct {
	idCol, keyCol, senderCol int
}

// resolveChatColumns applies the §SD1 contract to a schema. Pure and
// schema-only: every reject carries the reason the pane paints in place.
//
// `sender`, `body` and the identities carry no type requirement — they are
// read through formatCell, which is total over Arrow types. The times must be
// timestamps because a ClickHouse DateTime arrives as a bare integer that
// cannot be told from a count (the Timeline's rule), and the flags must be
// booleans because a string flag would need a vocabulary the contract does
// not have.
func resolveChatColumns(schema *arrow.Schema) (k chatClaim, reason string) {
	k = chatClaim{tsCol: -1, senderCol: -1, bodyCol: -1, idCol: -1, replyCol: -1, systemCol: -1,
		deletedCol: -1, editedCol: -1, statusCol: -1, convCol: -1, selRow: -1}
	if schema == nil {
		return k, chatContractHint(k)
	}
	for ci, f := range schema.Fields() {
		switch pathColumnLabel(f.Name) {
		case chatTsCol:
			ts, ok := f.Type.(*arrow.TimestampType)
			if !ok {
				return k, fmt.Sprintf("`%s` must be a DateTime or DateTime64; it is %s. toDateTime64(...) yields one.", f.Name, f.Type)
			}
			k.tsCol, k.tsUnit = ci, ts.Unit
		case chatSenderCol:
			k.senderCol = ci
		case chatBodyCol:
			k.bodyCol = ci
		case chatIdCol:
			k.idCol = ci
		case chatReplyToCol:
			k.replyCol = ci
		case chatSystemCol:
			if f.Type.ID() != arrow.BOOL {
				return k, fmt.Sprintf("`%s` must be a Bool; it is %s.", f.Name, f.Type)
			}
			k.systemCol = ci
		case chatDeletedCol:
			if f.Type.ID() != arrow.BOOL {
				return k, fmt.Sprintf("`%s` must be a Bool; it is %s.", f.Name, f.Type)
			}
			k.deletedCol = ci
		case chatEditedAtCol:
			ts, ok := f.Type.(*arrow.TimestampType)
			if !ok {
				return k, fmt.Sprintf("`%s` must be a DateTime or DateTime64; it is %s.", f.Name, f.Type)
			}
			k.editedCol, k.editedUnit = ci, ts.Unit
		case chatStatusCol:
			k.statusCol = ci
		case chatConversationCol:
			k.convCol = ci
		default:
			k.extraCols = append(k.extraCols, ci)
		}
	}
	if k.tsCol < 0 || k.senderCol < 0 || k.bodyCol < 0 {
		return k, chatContractHint(k)
	}
	return k, ""
}

// chatContractHint is the reject shown when a required column is absent —
// the pane's teaching moment, so it names the contract and a query that
// satisfies it.
func chatContractHint(k chatClaim) string {
	var missing []string
	if k.tsCol < 0 {
		missing = append(missing, "`"+chatTsCol+"`")
	}
	if k.senderCol < 0 {
		missing = append(missing, "`"+chatSenderCol+"`")
	}
	if k.bodyCol < 0 {
		missing = append(missing, "`"+chatBodyCol+"`")
	}
	return fmt.Sprintf("The chat needs a %s column. Name them in the query — e.g. SELECT sent_at AS ts, author AS sender, text AS body FROM t — "+
		"and optionally `id`, `reply_to`, `system`, `deleted`, `edited_at`, `status` and `conversation`; a `reactions` CTE (id, key, sender) and a `participants` CTE (sender, name, color) join in.",
		strings.Join(missing, ", a "))
}

// resolveChatRoster claims the participants channel: a `sender` column, and
// optionally `name` and `color`.
func resolveChatRoster(schema *arrow.Schema) (k chatRosterClaim, reason string) {
	k = chatRosterClaim{keyCol: -1, nameCol: -1, colorCol: -1}
	if schema == nil {
		return k, "no participants result"
	}
	for ci, f := range schema.Fields() {
		switch pathColumnLabel(f.Name) {
		case chatSenderCol:
			k.keyCol = ci
		case chatNameCol:
			k.nameCol = ci
		case chatColorCol:
			k.colorCol = ci
		}
	}
	if k.keyCol < 0 {
		return k, "the `participants` CTE needs a `sender` column (plus optional `name` and `color`)"
	}
	return k, ""
}

// resolveChatReactions claims the reactions channel: `id` and `key`, and
// optionally `sender`.
func resolveChatReactions(schema *arrow.Schema) (k chatReactionsClaim, reason string) {
	k = chatReactionsClaim{idCol: -1, keyCol: -1, senderCol: -1}
	if schema == nil {
		return k, "no reactions result"
	}
	for ci, f := range schema.Fields() {
		switch pathColumnLabel(f.Name) {
		case chatIdCol:
			k.idCol = ci
		case chatKeyCol:
			k.keyCol = ci
		case chatSenderCol:
			k.senderCol = ci
		}
	}
	if k.idCol < 0 || k.keyCol < 0 {
		return k, "the `reactions` CTE needs an `id` and a `key` column (plus an optional `sender`)"
	}
	return k, ""
}

// ChatDriver owns the Chat tab state: the folded model, the widget state,
// the two lanes for the optional CTEs, and the artifact cache the bubbles'
// block faces render from.
type ChatDriver struct {
	ids   *c.WidgetIdStack
	state chatview.State

	participantsLane *nodeLane
	reactionsLane    *nodeLane
	participantsErr  error
	reactionsErr     error
	participantsBusy bool
	reactionsBusy    bool

	// cache holds the block-face artifacts (a parsed markdown Doc, decoded
	// pixels, a highlighted job) keyed by (column, ordinal) — the Detail
	// pane's cache shape, but over the window's many rows rather than one.
	// Dropped whenever the main result changes.
	cache     *richCellCache
	cacheFor  ResultID
	cacheSeen bool

	// The fold and its cache key.
	model           *chatview.Model
	rows            []int64         // ordinal → Arrow row
	ordOf           map[int64]int32 // Arrow row → ordinal
	participantKeys []string        // participant index → sender key
	conversations   []string
	skipped         int
	truncated       int64
	unmatched       int // reactions naming no message
	forMain         ResultID
	forRoster       ResultID
	forReactions    ResultID
	forSchema       *arrow.Schema
	forConversation string
	folded          bool

	// The pickers (§SD2): the viewer by sender key ("" is nobody), the
	// conversation by value. Keys rather than indices, so they survive a
	// refold.
	viewer       string
	conversation string
}

// NewChatDriver builds the driver. client may be nil (tests, an unwired
// host): the CTE lanes are then absent and the transcript renders without a
// roster or reactions.
func NewChatDriver(ids *c.WidgetIdStack, client *Client) (inst *ChatDriver) {
	inst = &ChatDriver{ids: ids, cache: newRichCellCache(ids)}
	if client != nil {
		alloc := memory.NewGoAllocator()
		inst.participantsLane = newNodeLane(clientExecutor{client: client, opts: newExecOptions("chat-participants")}, alloc, 0)
		inst.reactionsLane = newNodeLane(clientExecutor{client: client, opts: newExecOptions("chat-reactions")}, alloc, 0)
	}
	return
}

// forgetLanes clears the CTE lanes' memo so the next demand re-executes —
// the Run hook, for the reason KanbanDriver.forgetLanes gives.
func (inst *ChatDriver) forgetLanes() {
	if inst == nil {
		return
	}
	if inst.participantsLane != nil {
		inst.participantsLane.forget()
	}
	if inst.reactionsLane != nil {
		inst.reactionsLane.forget()
	}
}

// close tears the lanes down.
func (inst *ChatDriver) close() {
	if inst == nil {
		return
	}
	if inst.participantsLane != nil {
		inst.participantsLane.close()
	}
	if inst.reactionsLane != nil {
		inst.reactionsLane.close()
	}
}

// chatPanel is the PanelI face. It carries the app rather than the driver
// alone because the bubbles' block faces come from the app's gloss
// resolution.
type chatPanel struct {
	app *PlayApp
}

func (inst chatPanel) ID() PanelID { return "chat" }

func (inst chatPanel) Channels() []ChannelSpec {
	return []ChannelSpec{
		{ID: chMain, Required: true, Label: "messages"},
		{ID: chParticipants, Required: false, Label: "participants"},
		{ID: chReactions, Required: false, Label: "reactions"},
	}
}

func (inst chatPanel) AcceptForChannel(ch ChannelID, schema *arrow.Schema, sig SignalEnvI) (claim ChannelClaim, reason string) {
	switch ch {
	case chParticipants:
		k, reason := resolveChatRoster(schema)
		if reason != "" {
			return nil, reason
		}
		return k, ""
	case chReactions:
		k, reason := resolveChatReactions(schema)
		if reason != "" {
			return nil, reason
		}
		return k, ""
	}
	if schema == nil {
		return nil, "Run a query to see the transcript."
	}
	k, reason := resolveChatColumns(schema)
	if reason != "" {
		return nil, reason
	}
	k.selRow, _ = readSelection(sig)
	return k, ""
}

func (inst chatPanel) Render(filled map[ChannelID]ChannelResult, emit SignalEmitterI) {
	main := filled[chMain]
	k, ok := main.Claim.(chatClaim)
	if !ok || main.Rec == nil {
		return
	}
	var roster *chatRosterInput
	if r, filledRoster := filled[chParticipants]; filledRoster && r.Rec != nil {
		if rk, isClaim := r.Claim.(chatRosterClaim); isClaim {
			roster = &chatRosterInput{rec: r.Rec, claim: rk, result: r.Result}
		}
	}
	var reactions *chatReactionsInput
	if r, filledReactions := filled[chReactions]; filledReactions && r.Rec != nil {
		if rk, isClaim := r.Claim.(chatReactionsClaim); isClaim {
			reactions = &chatReactionsInput{rec: r.Rec, claim: rk, result: r.Result}
		}
	}
	inst.app.chatDriver.render(inst.app, main.Rec, main.Result, k, roster, reactions, emit)
}

type chatRosterInput struct {
	rec    arrow.RecordBatch
	claim  chatRosterClaim
	result ResultID
}

type chatReactionsInput struct {
	rec    arrow.RecordBatch
	claim  chatReactionsClaim
	result ResultID
}

// render folds the result (cached), draws the pickers and the transcript,
// and carries the selection both ways.
func (inst *ChatDriver) render(app *PlayApp, rec arrow.RecordBatch, result ResultID, k chatClaim, roster *chatRosterInput, reactions *chatReactionsInput, emit SignalEmitterI) {
	schema := rec.Schema()
	inst.syncCache(result)
	inst.rebuild(rec, result, k, roster, reactions)
	m := inst.model
	dens := styletokens.ActiveDensity()

	inst.renderOptions(m, dens)
	if m == nil || m.Len() == 0 {
		for rt := range c.RichTextLabel("The query returned no messages, so the transcript is empty.") {
			rt.Small().Weak()
		}
		return
	}

	// Follow the shared selection before drawing, as the board does: the
	// transcript must not keep painting its own last click after the rest of
	// the dock moved on.
	if ord, ok := inst.ordOf[k.selRow]; ok && k.selRow >= 0 {
		inst.state.SetSelected(ord)
	} else {
		inst.state.SetSelected(-1)
	}

	cols := app.glossColumns(schema)
	rawCells := app.tableOpts.rawCells
	res := chatview.Render(chatview.Input{
		Ids:      inst.ids,
		ScopeKey: "play-chat",
		Model:    m,
		State:    &inst.state,
		Viewer:   inst.viewerIndex(),
		Location: time.Local,
		FillHost: true,
		Block: func(ord int) (chatview.Block, bool) {
			if rawCells || ord < 0 || ord >= len(inst.rows) {
				return chatview.Block{}, false
			}
			return inst.bubbleBlock(app, rec, schema, cols, k, ord)
		},
	})

	// Publish a click. Comparing against the claim's row means a selection
	// that merely echoes the signal back does not re-emit.
	if res.Clicked >= 0 && int(res.Clicked) < len(inst.rows) {
		if row := inst.rows[res.Clicked]; row != k.selRow {
			emit.Emit(signalSelection, row)
		}
	}
}

// syncCache drops the block-face artifacts when the main result changes.
func (inst *ChatDriver) syncCache(result ResultID) {
	if inst.cacheSeen && inst.cacheFor == result {
		return
	}
	inst.cacheSeen = true
	inst.cacheFor = result
	clear(inst.cache.entries)
	inst.cache.generation++
}

// viewerIndex resolves the viewer picker's key to a participant index, -1
// for nobody or a key the current fold does not carry.
func (inst *ChatDriver) viewerIndex() int32 {
	if inst.viewer == "" {
		return -1
	}
	for i, key := range inst.participantKeys {
		if key == inst.viewer {
			return int32(i)
		}
	}
	return -1
}

// renderOptions is the pane's options row: the status line, the viewer
// picker and — when the result carries more than one — the conversation
// picker.
func (inst *ChatDriver) renderOptions(m *chatview.Model, dens styletokens.DensityE) {
	ids := inst.ids
	for range c.HorizontalTop().KeepIter() {
		viewerLabel := chatNobody
		if v := inst.viewerIndex(); v >= 0 && m != nil {
			viewerLabel = m.Participants[v].Name
		}
		for range c.ComboBox(ids.PrepareStr("chat-viewer"),
			c.WidgetText().Text("viewer").Keep(),
			c.WidgetText().Text(viewerLabel).Keep()).KeepIter() {
			if c.Button(ids.PrepareSeq(0x4100), c.Atoms().Text(chatNobody).Keep()).
				Frame(false).Selected(inst.viewer == "").SendResp().HasPrimaryClicked() {
				inst.viewer = ""
			}
			if m != nil {
				for i, p := range m.Participants {
					key := inst.participantKeys[i]
					if c.Button(ids.PrepareSeq(uint64(0x4101+i)), c.Atoms().Text(p.Name).Keep()).
						Frame(false).Selected(inst.viewer == key).SendResp().HasPrimaryClicked() {
						inst.viewer = key
					}
				}
			}
		}
		if len(inst.conversations) > 1 {
			for range c.ComboBox(ids.PrepareStr("chat-conversation"),
				c.WidgetText().Text("conversation").Keep(),
				c.WidgetText().Text(inst.conversation).Keep()).KeepIter() {
				for i, name := range inst.conversations {
					if c.Button(ids.PrepareSeq(uint64(0x4400+i)), c.Atoms().Text(name).Keep()).
						Frame(false).Selected(inst.conversation == name).SendResp().HasPrimaryClicked() {
						inst.conversation = name
					}
				}
			}
		}
		for rt := range c.RichTextLabel(inst.statusLine()) {
			rt.Small().Weak()
		}
	}
	c.AddSpace(styletokens.GapInline(dens))
}

func (inst *ChatDriver) statusLine() string {
	var b strings.Builder
	if inst.model != nil {
		n := inst.model.Len()
		fmt.Fprintf(&b, "%d messages · %d participants", n, len(inst.model.Participants))
		if nr := len(inst.model.ReactionKey); nr > 0 {
			fmt.Fprintf(&b, " · %d reactions", nr)
		}
	}
	if inst.skipped > 0 {
		fmt.Fprintf(&b, " · %d rows skipped (null ts or sender)", inst.skipped)
	}
	if inst.truncated > 0 {
		fmt.Fprintf(&b, " · %s older rows not shown (the transcript caps at %d — add a WHERE or a LIMIT)",
			humanize.Comma(inst.truncated), chatMaxMessages)
	}
	if inst.unmatched > 0 {
		fmt.Fprintf(&b, " · %d reactions name no message", inst.unmatched)
	}
	switch {
	case inst.participantsErr != nil:
		fmt.Fprintf(&b, " · participants query failed: %v", inst.participantsErr)
	case inst.participantsBusy:
		b.WriteString(" · participants…")
	}
	switch {
	case inst.reactionsErr != nil:
		fmt.Fprintf(&b, " · reactions query failed: %v", inst.reactionsErr)
	case inst.reactionsBusy:
		b.WriteString(" · reactions…")
	}
	return b.String()
}

// chatEntry is one message before it is sorted into ordinals.
type chatEntry struct {
	row int64
	ms  int64
}

// rebuild folds the result into a chatview model, keyed on (main result,
// roster result, reactions result, schema, conversation).
func (inst *ChatDriver) rebuild(rec arrow.RecordBatch, result ResultID, k chatClaim, roster *chatRosterInput, reactions *chatReactionsInput) {
	var rosterID, reactionsID ResultID
	if roster != nil {
		rosterID = roster.result
	}
	if reactions != nil {
		reactionsID = reactions.result
	}
	// The conversation picker resolves against the result's own values, so
	// a stale pick from an earlier result falls back to the first one.
	conversations := chatConversations(rec, k)
	conversation := inst.conversation
	if len(conversations) > 0 && !containsString(conversations, conversation) {
		conversation = conversations[0]
	}
	if inst.folded && inst.forMain == result && inst.forRoster == rosterID && inst.forReactions == reactionsID &&
		inst.forSchema == rec.Schema() && inst.forConversation == conversation {
		return
	}
	inst.folded = true
	inst.forMain, inst.forRoster, inst.forReactions = result, rosterID, reactionsID
	inst.forSchema = rec.Schema()
	inst.forConversation = conversation
	inst.conversations = conversations
	inst.conversation = conversation

	m, rows, participantKeys, skipped, truncated, unmatched := foldChat(rec, k, roster, reactions, conversation, chatMaxMessages)
	inst.model, inst.rows, inst.participantKeys = m, rows, participantKeys
	inst.skipped, inst.truncated, inst.unmatched = skipped, truncated, unmatched
	inst.ordOf = make(map[int64]int32, len(rows))
	for ord, row := range rows {
		inst.ordOf[row] = int32(ord)
	}
	// A refold keeps following the tail and drops a selection that named
	// the old ordinals; the shared selection is re-projected every frame.
	inst.state.SetSelected(-1)
}

// chatConversations lists the distinct `conversation` values in first-seen
// order; nil when the result has no such column.
func chatConversations(rec arrow.RecordBatch, k chatClaim) (out []string) {
	if k.convCol < 0 {
		return nil
	}
	seen := make(map[string]struct{}, 4)
	for row := range rec.NumRows() {
		v := formatCell(rec, k.convCol, row)
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// foldChat is the pure fold: rows → a chatview model in ascending time,
// participants by first appearance, reactions aggregated per (message, key)
// in first-seen order. Returns the ordinal → row map, the participant keys,
// and the counts the status line reports. cap bounds the messages kept —
// the newest ones, since a transcript is read from its tail.
func foldChat(rec arrow.RecordBatch, k chatClaim, roster *chatRosterInput, reactions *chatReactionsInput, conversation string, cap int) (
	m *chatview.Model, rows []int64, participantKeys []string, skipped int, truncated int64, unmatched int) {
	tsArr, _ := rec.Column(k.tsCol).(*array.Timestamp)
	senderArr := rec.Column(k.senderCol)
	entries := make([]chatEntry, 0, rec.NumRows())
	for row := range rec.NumRows() {
		if tsArr == nil || tsArr.IsNull(int(row)) || senderArr.IsNull(int(row)) {
			skipped++
			continue
		}
		if k.convCol >= 0 && formatCell(rec, k.convCol, row) != conversation {
			continue
		}
		entries = append(entries, chatEntry{row: row, ms: tsToEpochMS(int64(tsArr.Value(int(row))), k.tsUnit)})
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].ms < entries[j].ms })
	if cap > 0 && len(entries) > cap {
		truncated = int64(len(entries) - cap)
		entries = entries[len(entries)-cap:]
	}

	n := len(entries)
	m = &chatview.Model{
		TimeMS:  make([]int64, n),
		Sender:  make([]int32, n),
		Body:    make([]string, n),
		ReplyTo: make([]int32, n),
		Flags:   make([]chatview.FlagsE, n),
		Status:  make([]chatview.StatusE, n),
	}
	rows = make([]int64, n)

	// The roster: display names and colours by sender key.
	type rosterEntry struct {
		name  string
		color color.Color
	}
	rosterOf := map[string]rosterEntry{}
	if roster != nil && roster.rec != nil {
		for row := range roster.rec.NumRows() {
			key := formatCell(roster.rec, roster.claim.keyCol, row)
			e := rosterEntry{}
			if roster.claim.nameCol >= 0 {
				e.name = formatCell(roster.rec, roster.claim.nameCol, row)
			}
			if roster.claim.colorCol >= 0 {
				if tok, ok := bandColorTokens[strings.ToLower(strings.TrimSpace(formatCell(roster.rec, roster.claim.colorCol, row)))]; ok {
					e.color = color.Hex(tok.AsHex())
				}
			}
			if _, dup := rosterOf[key]; !dup {
				rosterOf[key] = e
			}
		}
	}
	participantOf := map[string]int32{}
	participant := func(key string) int32 {
		if idx, ok := participantOf[key]; ok {
			return idx
		}
		idx := int32(len(m.Participants))
		participantOf[key] = idx
		participantKeys = append(participantKeys, key)
		p := chatview.Participant{Name: key}
		if e, ok := rosterOf[key]; ok {
			if e.name != "" {
				p.Name = e.name
			}
			p.Color = e.color
		}
		if p.Name == "" {
			p.Name = "(unknown)"
		}
		m.Participants = append(m.Participants, p)
		return idx
	}

	var (
		systemArr, deletedArr *array.Boolean
		editedArr             *array.Timestamp
	)
	if k.systemCol >= 0 {
		systemArr, _ = rec.Column(k.systemCol).(*array.Boolean)
	}
	if k.deletedCol >= 0 {
		deletedArr, _ = rec.Column(k.deletedCol).(*array.Boolean)
	}
	if k.editedCol >= 0 {
		editedArr, _ = rec.Column(k.editedCol).(*array.Timestamp)
		m.EditedMS = make([]int64, n)
	}
	// Message identities: the `id` column's text, else the row number.
	ordOfID := make(map[string]int32, n)
	idOf := func(row int64) string {
		if k.idCol >= 0 {
			return formatCell(rec, k.idCol, row)
		}
		return strconv.FormatInt(row, 10)
	}
	for ord, e := range entries {
		row := e.row
		rows[ord] = row
		m.TimeMS[ord] = e.ms
		m.ReplyTo[ord] = -1
		if _, dup := ordOfID[idOf(row)]; !dup {
			ordOfID[idOf(row)] = int32(ord)
		}
		system := systemArr != nil && !systemArr.IsNull(int(row)) && systemArr.Value(int(row))
		if system {
			m.Flags[ord] |= chatview.FlagSystem
			m.Sender[ord] = -1
		} else {
			m.Sender[ord] = participant(formatCell(rec, k.senderCol, row))
		}
		// Cloned: formatCell may hand back a string aliasing the Arrow
		// buffer, and the model outlives the frame.
		m.Body[ord] = strings.Clone(formatCell(rec, k.bodyCol, row))
		if deletedArr != nil && !deletedArr.IsNull(int(row)) && deletedArr.Value(int(row)) {
			m.Flags[ord] |= chatview.FlagDeleted
		}
		if editedArr != nil && !editedArr.IsNull(int(row)) {
			m.Flags[ord] |= chatview.FlagEdited
			m.EditedMS[ord] = tsToEpochMS(int64(editedArr.Value(int(row))), k.editedUnit)
		}
		if k.statusCol >= 0 {
			m.Status[ord] = chatStatusOf(formatCell(rec, k.statusCol, row))
		}
	}
	if k.replyCol >= 0 {
		replyArr := rec.Column(k.replyCol)
		for ord, e := range entries {
			if replyArr.IsNull(int(e.row)) {
				continue
			}
			if q, ok := ordOfID[formatCell(rec, k.replyCol, e.row)]; ok && int(q) != ord {
				m.ReplyTo[ord] = q
			}
		}
	}

	// Reactions: aggregate per (message, key) in first-seen order, then lay
	// them out as a ragged co-array.
	if reactions != nil && reactions.rec != nil && reactions.rec.NumRows() > 0 {
		type agg struct {
			key   string
			count int32
			who   []string
		}
		per := make(map[int32][]*agg, 16)
		rr := reactions.rec
		for row := range rr.NumRows() {
			ord, ok := ordOfID[formatCell(rr, reactions.claim.idCol, row)]
			if !ok {
				unmatched++
				continue
			}
			key := strings.Clone(formatCell(rr, reactions.claim.keyCol, row))
			var a *agg
			for _, cand := range per[ord] {
				if cand.key == key {
					a = cand
					break
				}
			}
			if a == nil {
				a = &agg{key: key}
				per[ord] = append(per[ord], a)
			}
			a.count++
			if reactions.claim.senderCol >= 0 {
				if who := formatCell(rr, reactions.claim.senderCol, row); who != "" {
					a.who = append(a.who, strings.Clone(who))
				}
			}
		}
		m.ReactionOff = make([]int32, n+1)
		for ord := range n {
			m.ReactionOff[ord] = int32(len(m.ReactionKey))
			for _, a := range per[int32(ord)] {
				m.ReactionKey = append(m.ReactionKey, a.key)
				m.ReactionCount = append(m.ReactionCount, a.count)
				m.ReactionWho = append(m.ReactionWho, strings.Join(a.who, ", "))
			}
		}
		m.ReactionOff[n] = int32(len(m.ReactionKey))
	}
	return
}

// chatStatusOf reads the `status` vocabulary, case-insensitively; anything
// else is no status.
func chatStatusOf(s string) chatview.StatusE {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "sent":
		return chatview.StatusSent
	case "delivered":
		return chatview.StatusDelivered
	case "read":
		return chatview.StatusRead
	case "failed":
		return chatview.StatusFailed
	}
	return chatview.StatusNone
}

// bubbleBlock is the host-drawn body of one bubble (§SD1): the `body` column
// through its gloss when that gloss has a block face, then every unclaimed
// column — a block face where the gloss has one, the inline face where it
// does not, a caption line where the column is plain. Declined when the body
// is plain and nothing else is there, so the widget draws the text itself.
func (inst *ChatDriver) bubbleBlock(app *PlayApp, rec arrow.RecordBatch, schema *arrow.Schema, cols []glossColumn, k chatClaim, ord int) (chatview.Block, bool) {
	row := inst.rows[ord]
	var parts []func()
	if k.bodyCol < len(cols) && cardBlockFace(&cols[k.bodyCol]) {
		if part, ok := inst.blockPart(app, rec, schema, &cols[k.bodyCol], k.bodyCol, row, ord, ""); ok {
			parts = append(parts, part)
		}
	}
	for _, ci := range k.extraCols {
		if ci >= len(cols) || rec.Column(ci).IsNull(int(row)) {
			continue
		}
		gc := &cols[ci]
		caption := shortColumnLabel(schema.Field(ci).Name)
		if gc.label != "" {
			caption = gc.label
		}
		switch {
		case cardBlockFace(gc):
			if part, ok := inst.blockPart(app, rec, schema, gc, ci, row, ord, caption); ok {
				parts = append(parts, part)
			}
		case gc.glossed():
			text, tone := app.glossCell(gc, rec.Column(ci), row, false)
			text = strings.Clone(text)
			parts = append(parts, func() { chatCaptionLine(caption, text, tone) })
		default:
			text := strings.Clone(formatCell(rec, ci, row))
			if text == "" {
				continue
			}
			parts = append(parts, func() { chatCaptionLine(caption, text, gloss.ToneNeutral) })
		}
	}
	if len(parts) == 0 {
		return chatview.Block{}, false
	}
	return chatview.Block{Render: func() {
		for i, part := range parts {
			for range c.PushId(inst.ids.PrepareSeq(uint64(0x4800 + i))).KeepIter() {
				part()
			}
		}
	}}, true
}

// blockPart is one block face inside a bubble, from the shared artifact
// cache (built on first sight, keyed by column and ordinal). The raw cell
// aliases Arrow memory, so it is cloned before the cache retains anything
// derived from it.
func (inst *ChatDriver) blockPart(app *PlayApp, rec arrow.RecordBatch, schema *arrow.Schema, gc *glossColumn, ci int, row int64, ord int, caption string) (func(), bool) {
	raw, ok := cellRaw(rec, ci, row)
	if !ok || raw == "" {
		return nil, false
	}
	key := richKey{col: ci, ord: ord}
	if _, cached := inst.cache.entries[key]; !cached {
		raw = strings.Clone(raw)
	}
	kind := gloss.KindOfArrow(listElemType(schema.Field(ci).Type))
	block := app.glossBlock(inst.cache, "chat", gc, key, raw, kind)
	if block.Render == nil {
		return nil, false
	}
	return func() {
		if caption != "" {
			for rt := range c.RichTextLabel(caption) {
				rt.Small().Weak()
			}
		}
		block.Render()
	}, true
}

// chatCaptionLine is an unclaimed column's one-line rendering in the bubble:
// the caption weak, the value in its tone.
func chatCaptionLine(caption, text string, tone gloss.ToneE) {
	for range c.HorizontalTop().KeepIter() {
		for rt := range c.RichTextLabel(caption + ":") {
			rt.Small().Weak()
		}
		if col, toned := toneColor(tone); toned {
			for rt := range c.RichTextLabelColored(col, color.Transparent, text) {
				rt.Small()
			}
		} else {
			c.LabelAtoms(c.Atoms().BeginRichText(text).Small().End().Keep()).Wrap().Selectable(false).Send()
		}
	}
}

// renderChatTab is the Chat dock tab body (ADR-0239): the active result as a
// transcript. The same guards as the Kanban tab; the two optional CTEs are
// demanded on their own lanes and offered as channels.
func (inst *PlayApp) renderChatTab(rec arrow.RecordBatch, schema *arrow.Schema, loading bool, err error, result ResultID) {
	if loading && rec == nil {
		inst.renderResultsLoading()
		return
	}
	if err != nil && rec == nil {
		inst.renderResultsFailed()
		return
	}
	if rec == nil {
		for rt := range c.RichTextLabel("Run a query naming `ts`, `sender` and `body` columns to see a transcript.") {
			rt.Small().Weak()
		}
		return
	}
	d := inst.chatDriver
	inputs := map[ChannelID]channelInput{
		chMain: {node: inst.resolvedTabNode("chat"), rec: rec, schema: schema, sig: inst.frameSig, result: result},
	}
	if view, present := inst.demandChatCTE(d.participantsLane, chatParticipantsNodeID); present {
		d.participantsBusy, d.participantsErr = view.loading, view.err
		if view.rec != nil {
			defer view.rec.Release()
		}
		if view.rec != nil || view.schema != nil {
			inputs[chParticipants] = channelInput{node: chatParticipantsNodeID, rec: view.rec, schema: view.schema, sig: inst.frameSig, result: view.id}
		}
	} else {
		d.participantsBusy, d.participantsErr = false, nil
	}
	if view, present := inst.demandChatCTE(d.reactionsLane, chatReactionsNodeID); present {
		d.reactionsBusy, d.reactionsErr = view.loading, view.err
		if view.rec != nil {
			defer view.rec.Release()
		}
		if view.rec != nil || view.schema != nil {
			inputs[chReactions] = channelInput{node: chatReactionsNodeID, rec: view.rec, schema: view.schema, sig: inst.frameSig, result: view.id}
		}
	} else {
		d.reactionsBusy, d.reactionsErr = false, nil
	}
	reject := dispatchPanel(chatPanel{app: inst}, inputs, inst.sigEmit)
	if reject != "" {
		for rt := range c.RichTextLabel(reject) {
			rt.Small().Weak()
		}
	}
}

// demandChatCTE compiles one of the transcript query's optional CTEs — if
// the buffer has it — and demands it on the given lane, returning the
// retained view (the caller MUST Release view.rec). present is false when the
// lane is absent or the buffer carries no such CTE. The shape of
// demandKanbanLanes, over a lane and a node id.
func (inst *PlayApp) demandChatCTE(lane *nodeLane, id NodeID) (view laneView, present bool) {
	if lane == nil {
		return
	}
	node, ok := findSplitNode(inst.currentSplit, id)
	if !ok {
		return
	}
	view = lane.demand(compiledNode{
		SQL:    fuseNode(inst.currentSplit, id),
		NodeID: id,
		Params: resolveSignalNamesWithDefaults(node.Reads, inst.lastRunBound, inst.frameSig),
	})
	return view, true
}
