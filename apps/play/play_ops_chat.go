package play

import (
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/chatview"
)

// The Chat pane as an agent reads it (ADR-0270, update of 2026-10-05): the
// transcript the pane folded — in time order, filtered to its conversation,
// with names from the roster, replies threaded to ordinals, reactions
// aggregated per message — and what the fold left out: rows skipped for a
// null time or sender, rows past the cap, reactions naming no message, and
// why the optional participants and reactions CTEs did not join in. The
// fold is replaced, never edited, so the snapshot shares it. The pane has
// no command: its viewer and conversation pickers stay the person's, and a
// message is selected with set_signal('selection', row).
//
// deferred: a set_chat_pane_options for the viewer and conversation pickers,
// until a task shows the need for one.

const (
	opGetChatPane        = "get_chat_pane"
	chatPaneId           = "chat"
	chatReadDefaultLimit = 30
	chatReadMaxLimit     = 50
	chatReadBodyRunes    = 1000
)

// ChatMessageReading is one message of the transcript.
type ChatMessageReading struct {
	Ordinal   int32    `desc:"its place in the transcript, 0 for the oldest message kept"`
	Row       int64    `desc:"its result row; set_signal('selection', row) selects it"`
	Time      string   `desc:"when it was sent, UTC"`
	Sender    string   `json:",omitzero" desc:"the sender key; absent on a system line"`
	Name      string   `json:",omitzero" desc:"the sender's name, from the participants CTE where it names one"`
	Body      string   `desc:"the body as plain text (markdown flattened), cut at 1000 characters"`
	BodyCut   bool     `json:",omitzero" desc:"the body was cut"`
	ReplyTo   *int32   `json:",omitzero" desc:"the ordinal of the message it replies to"`
	System    bool     `json:",omitzero" desc:"a system line rather than a message"`
	Deleted   bool     `json:",omitzero" desc:"retracted; the pane shows a placeholder"`
	Edited    string   `json:",omitzero" desc:"when it was edited, UTC"`
	Status    string   `json:",omitzero" desc:"sent, delivered, read or failed"`
	Reactions []string `json:",omitzero" desc:"each reaction as key×count, with who reacted in parentheses when known"`
}

// ChatParticipantReading is one participant.
type ChatParticipantReading struct {
	Sender string `desc:"the sender key"`
	Name   string `desc:"the name the pane shows"`
}

// ChatPaneReading is get_chat_pane's result.
type ChatPaneReading struct {
	Drawn             PaneDraw                 `desc:"which draw this is of, its status line, and why it drew nothing when it did not"`
	Messages          int32                    `desc:"messages in the transcript"`
	Skipped           int32                    `json:",omitzero" desc:"rows skipped for a null ts or sender"`
	NotShown          int64                    `json:",omitzero" desc:"older rows past the transcript's cap of 5000 messages"`
	Unmatched         int32                    `json:",omitzero" desc:"reactions naming no message of the transcript"`
	Participants      string                   `desc:"the participants CTE: joined, absent (the buffer has none), running, failed with why, or not usable with why"`
	Reactions         string                   `desc:"the reactions CTE, as participants"`
	Conversations     []string                 `json:",omitzero" desc:"the result's conversations, when it has a conversation column; at most 50"`
	MoreConversations int32                    `json:",omitzero" desc:"conversations past the 50 listed"`
	Conversation      string                   `json:",omitzero" desc:"the conversation the transcript shows"`
	Viewer            string                   `json:",omitzero" desc:"the sender whose messages sit on the reader's side; absent for nobody"`
	Roster            []ChatParticipantReading `json:",omitzero" desc:"the participants in order of first appearance; at most 100"`
	MoreRoster        int32                    `json:",omitzero" desc:"participants past the 100 listed"`
	List              []ChatMessageReading     `json:",omitzero" desc:"the messages read, oldest first"`
	Before            int32                    `json:",omitzero" desc:"messages older than the ones listed"`
	After             int32                    `json:",omitzero" desc:"messages newer than the ones listed"`
}

// GetChatPaneArgs is get_chat_pane's argument.
type GetChatPaneArgs struct {
	Offset *int32 `json:",omitzero" desc:"the ordinal to read from; left out, the newest messages"`
	Limit  int32  `json:",omitzero" desc:"messages to read, 30 by default and at most 50"`
}

// chatOpsView is what get_chat_pane reads: the fold, shared, and the pane's
// counts and lane states.
type chatOpsView struct {
	model           *chatview.Model
	rows            []int64
	participantKeys []string
	conversations   []string
	conversation    string
	viewer          string
	skipped         int
	truncated       int64
	unmatched       int
	participants    string
	reactions       string
}

// chatChannelState says what became of one optional CTE.
func chatChannelState(present bool, busy bool, err error, reject string) string {
	switch {
	case !present:
		return "absent"
	case err != nil:
		return "failed: " + err.Error()
	case busy:
		return "running"
	case reject != "":
		return "not usable: " + reject
	}
	return "joined"
}

func (inst *PlayApp) chatView() chatOpsView {
	d := inst.chatDriver
	return chatOpsView{model: d.model, rows: d.rows, participantKeys: d.participantKeys, conversations: d.conversations,
		conversation: d.conversation, viewer: d.viewer, skipped: d.skipped, truncated: d.truncated, unmatched: d.unmatched,
		participants: chatChannelState(d.participantsPresent, d.participantsBusy, d.participantsErr, d.participantsReject),
		reactions:    chatChannelState(d.reactionsPresent, d.reactionsBusy, d.reactionsErr, d.reactionsReject)}
}

func chatStatusName(s chatview.StatusE) string {
	switch s {
	case chatview.StatusSent:
		return "sent"
	case chatview.StatusDelivered:
		return "delivered"
	case chatview.StatusRead:
		return "read"
	case chatview.StatusFailed:
		return "failed"
	}
	return ""
}

func chatTimeText(ms int64) string { return time.UnixMilli(ms).UTC().Format("2006-01-02 15:04:05.000") }

// chatMessage reads message ord of the fold.
func (inst *chatOpsView) chatMessage(ord int) (r ChatMessageReading) {
	m := inst.model
	r = ChatMessageReading{Ordinal: int32(ord), Time: chatTimeText(m.TimeMS[ord])}
	if ord < len(inst.rows) {
		r.Row = inst.rows[ord]
	}
	r.Body = truncateRunes(m.Body[ord], chatReadBodyRunes)
	r.BodyCut = utf8.RuneCountInString(m.Body[ord]) > chatReadBodyRunes
	if s := m.Sender[ord]; s >= 0 && int(s) < len(m.Participants) {
		if int(s) < len(inst.participantKeys) {
			r.Sender = opsLabel(inst.participantKeys[s])
		}
		if name := opsLabel(m.Participants[s].Name); name != r.Sender {
			r.Name = name
		}
	}
	if q := m.ReplyTo[ord]; q >= 0 {
		r.ReplyTo = &q
	}
	f := m.Flags[ord]
	r.System, r.Deleted = f&chatview.FlagSystem != 0, f&chatview.FlagDeleted != 0
	if f&chatview.FlagEdited != 0 && ord < len(m.EditedMS) {
		r.Edited = chatTimeText(m.EditedMS[ord])
	}
	r.Status = chatStatusName(m.Status[ord])
	if len(m.ReactionOff) > ord+1 {
		for j := m.ReactionOff[ord]; j < m.ReactionOff[ord+1] && j < m.ReactionOff[ord]+chatReadMaxReactions; j++ {
			s := opsLabel(m.ReactionKey[j]) + "×" + strconv.Itoa(int(m.ReactionCount[j]))
			if who := m.ReactionWho[j]; who != "" {
				s += " (" + truncateBytes(who, 200) + ")"
			}
			r.Reactions = append(r.Reactions, s)
		}
	}
	return
}

// The roster, the conversation list and a message's reactions are bounded
// so a large participants or reactions CTE cannot grow a reading without
// limit; what is past the bound is counted (review finding).
const (
	chatReadMaxRoster        = 100
	chatReadMaxConversations = 50
	chatReadMaxReactions     = 20
)

// chatPaneReading is get_chat_pane.
func chatPaneReading(sn *opsSnap, in GetChatPaneArgs) (out ChatPaneReading, err error) {
	d, readable, err := paneDrawOf(sn, chatPaneId)
	if err != nil {
		return
	}
	v := &sn.paneViews.chat
	out = ChatPaneReading{Drawn: d, Participants: v.participants, Reactions: v.reactions}
	if !readable || v.model == nil {
		return
	}
	m := v.model
	n := m.Len()
	out.Messages, out.Skipped, out.NotShown, out.Unmatched = int32(n), int32(v.skipped), v.truncated, int32(v.unmatched)
	for i, c := range v.conversations {
		if i == chatReadMaxConversations {
			out.MoreConversations = int32(len(v.conversations) - i)
			break
		}
		out.Conversations = append(out.Conversations, opsLabel(c))
	}
	out.Conversation = opsLabel(v.conversation)
	out.Viewer = opsLabel(v.viewer)
	for i, p := range m.Participants {
		if i == chatReadMaxRoster {
			out.MoreRoster = int32(len(m.Participants) - i)
			break
		}
		r := ChatParticipantReading{Name: opsLabel(p.Name)}
		if i < len(v.participantKeys) {
			r.Sender = opsLabel(v.participantKeys[i])
		}
		out.Roster = append(out.Roster, r)
	}
	limit := int(in.Limit)
	switch {
	case limit < 0 || limit > chatReadMaxLimit:
		return out, app.RefuseOperation("limit is at most " + strconv.Itoa(chatReadMaxLimit))
	case limit == 0:
		limit = chatReadDefaultLimit
	}
	from := max(n-limit, 0)
	if in.Offset != nil {
		from = int(*in.Offset)
		if from < 0 || (from > 0 && from >= n) {
			return out, app.RefuseOperation("the transcript has " + strconv.Itoa(n) + " messages; offset " + strconv.Itoa(from) + " is past them")
		}
	}
	// The newest first when reading the tail, so the byte bound cuts the
	// oldest; listed oldest first either way.
	var list []ChatMessageReading
	used := 0
	to := min(from+limit, n)
	if in.Offset == nil {
		for ord := n - 1; ord >= from; ord-- {
			r := v.chatMessage(ord)
			used += chatMessageBytes(&r)
			if used > opsSampleMaxBytes && ord < n-1 {
				from = ord + 1
				break
			}
			list = append(list, r)
		}
		for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
			list[i], list[j] = list[j], list[i]
		}
	} else {
		for ord := from; ord < to; ord++ {
			r := v.chatMessage(ord)
			used += chatMessageBytes(&r)
			if used > opsSampleMaxBytes && ord > from {
				to = ord
				break
			}
			list = append(list, r)
		}
	}
	out.List = list
	out.Before = int32(from)
	out.After = int32(n - from - len(list))
	return
}

func chatMessageBytes(r *ChatMessageReading) (n int) {
	n = 96 + len(r.Sender) + len(r.Name) + len(r.Body)
	for _, s := range r.Reactions {
		n += len(s) + 4
	}
	return
}

func addChatPaneOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	addPaneOps(s, paneOpsSpec[ChatPaneReading, GetChatPaneArgs, struct{}]{
		pane: chatPaneId,
		get:  opGetChatPane,
		getSummary: "read the transcript the Chat pane folded from its result: messages in time order with sender names, " +
			"replies, flags and reactions, what the fold skipped or cut, and whether the participants and reactions CTEs joined in",
		read: chatPaneReading,
	})
}
