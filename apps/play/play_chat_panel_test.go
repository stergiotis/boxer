package play

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/chatview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ADR-0239 §Verification: the transcript's schema contract (§SD1), the fold's
// ordering and joins, and the reject prose.

var chatTsType = &arrow.TimestampType{Unit: arrow.Millisecond, TimeZone: "UTC"}

func chatTsField(n string) arrow.Field { return arrow.Field{Name: n, Type: chatTsType} }
func boolField(n string) arrow.Field {
	return arrow.Field{Name: n, Type: arrow.FixedWidthTypes.Boolean}
}

// chatCol is one column of a test record: a name, and values as strings
// (nil = NULL) coerced by the field's type.
type chatCol struct {
	field arrow.Field
	vals  []any
}

func chatRec(t *testing.T, cols ...chatCol) arrow.RecordBatch {
	t.Helper()
	alloc := memory.NewGoAllocator()
	fields := make([]arrow.Field, 0, len(cols))
	arrs := make([]arrow.Array, 0, len(cols))
	n := -1
	for _, col := range cols {
		if n < 0 {
			n = len(col.vals)
		}
		require.Len(t, col.vals, n)
		fields = append(fields, col.field)
		switch col.field.Type.(type) {
		case *arrow.TimestampType:
			b := array.NewTimestampBuilder(alloc, col.field.Type.(*arrow.TimestampType))
			for _, v := range col.vals {
				if v == nil {
					b.AppendNull()
				} else {
					b.Append(arrow.Timestamp(v.(int64)))
				}
			}
			arrs = append(arrs, b.NewArray())
			b.Release()
		case *arrow.BooleanType:
			b := array.NewBooleanBuilder(alloc)
			for _, v := range col.vals {
				if v == nil {
					b.AppendNull()
				} else {
					b.Append(v.(bool))
				}
			}
			arrs = append(arrs, b.NewArray())
			b.Release()
		default:
			b := array.NewStringBuilder(alloc)
			for _, v := range col.vals {
				if v == nil {
					b.AppendNull()
				} else {
					b.Append(v.(string))
				}
			}
			arrs = append(arrs, b.NewArray())
			b.Release()
		}
	}
	return array.NewRecordBatch(arrow.NewSchema(fields, nil), arrs, int64(n))
}

func TestChatAcceptContract(t *testing.T) {
	p := chatPanel{app: &PlayApp{}}

	_, reason := p.AcceptForChannel(chMain, schemaWith(strField("sender"), strField("body")), nil)
	assert.Contains(t, reason, "`ts`")
	assert.Contains(t, reason, "SELECT sent_at AS ts", "the reject names a query that satisfies the contract")

	_, reason = p.AcceptForChannel(chMain, schemaWith(strField("ts"), strField("sender"), strField("body")), nil)
	assert.Contains(t, reason, "`ts` must be a DateTime")

	_, reason = p.AcceptForChannel(chMain, schemaWith(chatTsField("ts"), strField("sender"), strField("body"), strField("system")), nil)
	assert.Contains(t, reason, "`system` must be a Bool")

	// A glossed body still claims `body`; unclaimed columns are kept in order.
	claim, reason := p.AcceptForChannel(chMain, schemaWith(chatTsField("ts"), strField("photo@image/png"),
		strField("sender"), strField("body@text/markdown"), strField("size@gloss/bytes")), nil)
	require.Empty(t, reason)
	k, ok := claim.(chatClaim)
	require.True(t, ok)
	assert.Equal(t, 0, k.tsCol)
	assert.Equal(t, arrow.Millisecond, k.tsUnit)
	assert.Equal(t, 2, k.senderCol)
	assert.Equal(t, 3, k.bodyCol)
	assert.Equal(t, []int{1, 4}, k.extraCols)
	assert.Equal(t, int64(-1), k.selRow)

	_, reason = p.AcceptForChannel(chParticipants, schemaWith(strField("name")), nil)
	assert.Contains(t, reason, "`participants` CTE needs a `sender`")
	rc, reason := p.AcceptForChannel(chParticipants, schemaWith(strField("sender"), strField("color")), nil)
	require.Empty(t, reason)
	assert.Equal(t, chatRosterClaim{keyCol: 0, nameCol: -1, colorCol: 1}, rc)

	_, reason = p.AcceptForChannel(chReactions, schemaWith(strField("id")), nil)
	assert.Contains(t, reason, "`reactions` CTE needs an `id` and a `key`")
	xc, reason := p.AcceptForChannel(chReactions, schemaWith(strField("key"), strField("id")), nil)
	require.Empty(t, reason)
	assert.Equal(t, chatReactionsClaim{idCol: 1, keyCol: 0, senderCol: -1}, xc)

	// The panel's channel set.
	ids := make([]ChannelID, 0, 3)
	for _, ch := range p.Channels() {
		ids = append(ids, ch.ID)
	}
	assert.Equal(t, []ChannelID{chMain, chParticipants, chReactions}, ids)
}

// The fold: rows sorted by time, identities resolved, flags read, the roster
// and the reactions joined, and every drop counted.
func TestChatFold(t *testing.T) {
	rec := chatRec(t,
		chatCol{chatTsField("ts"), []any{int64(3000), int64(1000), int64(2000), nil, int64(4000), int64(5000)}},
		chatCol{strField("sender"), []any{"bob", "ada", "ada", "ada", nil, "bob"}},
		chatCol{strField("body"), []any{"yes", "hi", "there?", "lost", "ghost", "bye"}},
		chatCol{strField("id"), []any{"m3", "m1", "m2", "m9", "m8", "m4"}},
		chatCol{strField("reply_to"), []any{"m2", nil, nil, nil, nil, "m404"}},
		chatCol{boolField("system"), []any{false, false, false, false, false, true}},
		chatCol{boolField("deleted"), []any{false, false, true, false, false, false}},
		chatCol{chatTsField("edited_at"), []any{int64(3500), nil, nil, nil, nil, nil}},
		chatCol{strField("status"), []any{"READ", "sent", "", "", "", ""}},
	)
	defer rec.Release()
	k, reason := resolveChatColumns(rec.Schema())
	require.Empty(t, reason)

	roster := &chatRosterInput{
		rec: chatRec(t,
			chatCol{strField("sender"), []any{"ada", "cy"}},
			chatCol{strField("name"), []any{"Ada Lovelace", "Cy"}},
			chatCol{strField("color"), []any{"info.default", "nope"}},
		),
		claim: chatRosterClaim{keyCol: 0, nameCol: 1, colorCol: 2},
	}
	defer roster.rec.Release()
	reactions := &chatReactionsInput{
		rec: chatRec(t,
			chatCol{strField("id"), []any{"m3", "m3", "m3", "m1", "m404"}},
			chatCol{strField("key"), []any{"👍", "❤️", "👍", "👍", "👍"}},
			chatCol{strField("sender"), []any{"ada", "ada", "cy", "bob", "bob"}},
		),
		claim: chatReactionsClaim{idCol: 0, keyCol: 1, senderCol: 2},
	}
	defer reactions.rec.Release()

	m, rows, keys, skipped, truncated, unmatched := foldChat(rec, k, roster, reactions, "", 0)
	require.NoError(t, m.Validate())
	assert.Equal(t, 2, skipped, "a null ts and a null sender")
	assert.Zero(t, truncated)
	assert.Equal(t, 1, unmatched)

	// Ascending time; the ordinal → row map follows.
	assert.Equal(t, []int64{1000, 2000, 3000, 5000}, m.TimeMS)
	assert.Equal(t, []int64{1, 2, 0, 5}, rows)
	assert.Equal(t, []string{"hi", "there?", "yes", "bye"}, m.Body)

	// Participants by first appearance in time order, named from the roster.
	assert.Equal(t, []string{"ada", "bob"}, keys)
	require.Len(t, m.Participants, 2)
	assert.Equal(t, "Ada Lovelace", m.Participants[0].Name)
	assert.Equal(t, color.Hex(styletokens.InfoDefault.AsHex()), m.Participants[0].Color)
	assert.Equal(t, "bob", m.Participants[1].Name, "a sender the roster does not name shows its key")
	assert.Equal(t, color.ColorKindNone, m.Participants[1].Color.Kind())

	// Identities: the reply resolves to the quoted message's ordinal; a
	// reply to an unknown id is no reply. The system row has no sender.
	assert.Equal(t, []int32{-1, -1, 1, -1}, m.ReplyTo)
	assert.Equal(t, []int32{0, 0, 1, -1}, m.Sender)
	assert.Equal(t, chatview.FlagSystem, m.Flags[3])
	assert.Equal(t, chatview.FlagDeleted, m.Flags[1])
	assert.Equal(t, chatview.FlagEdited, m.Flags[2])
	assert.Equal(t, int64(3500), m.EditedMS[2])
	assert.Equal(t, []chatview.StatusE{chatview.StatusSent, chatview.StatusNone, chatview.StatusRead, chatview.StatusNone}, m.Status)

	// Reactions: aggregated per (message, key) in first-seen order.
	assert.Equal(t, []int32{0, 1, 1, 3, 3}, m.ReactionOff)
	assert.Equal(t, []string{"👍", "👍", "❤️"}, m.ReactionKey)
	assert.Equal(t, []int32{1, 2, 1}, m.ReactionCount)
	assert.Equal(t, []string{"bob", "ada, cy", "ada"}, m.ReactionWho)
	keysOf, counts, who := m.Reactions(2)
	assert.Equal(t, []string{"👍", "❤️"}, keysOf)
	assert.Equal(t, []int32{2, 1}, counts)
	assert.Equal(t, []string{"ada, cy", "ada"}, who)
}

// Without an `id` column the row number is the identity, which is what a
// `reactions` CTE built with rowNumberInAllBlocks() names.
func TestChatFoldRowNumberIdentity(t *testing.T) {
	rec := chatRec(t,
		chatCol{chatTsField("ts"), []any{int64(1), int64(2)}},
		chatCol{strField("sender"), []any{"a", "b"}},
		chatCol{strField("body"), []any{"x", "y"}},
		chatCol{strField("reply_to"), []any{nil, "0"}},
	)
	defer rec.Release()
	k, reason := resolveChatColumns(rec.Schema())
	require.Empty(t, reason)
	reactions := &chatReactionsInput{
		rec:   chatRec(t, chatCol{strField("id"), []any{"1"}}, chatCol{strField("key"), []any{"ok"}}),
		claim: chatReactionsClaim{idCol: 0, keyCol: 1, senderCol: -1},
	}
	defer reactions.rec.Release()
	m, _, _, _, _, unmatched := foldChat(rec, k, nil, reactions, "", 0)
	require.NoError(t, m.Validate())
	assert.Zero(t, unmatched)
	assert.Equal(t, []int32{-1, 0}, m.ReplyTo)
	assert.Equal(t, []int32{0, 0, 1}, m.ReactionOff)
	assert.Equal(t, []string{""}, m.ReactionWho, "no sender column: nobody to list")
}

// The cap keeps the newest messages; the conversation picker filters rows.
func TestChatFoldCapAndConversation(t *testing.T) {
	rec := chatRec(t,
		chatCol{chatTsField("ts"), []any{int64(1), int64(2), int64(3), int64(4)}},
		chatCol{strField("sender"), []any{"a", "a", "a", "b"}},
		chatCol{strField("body"), []any{"1", "2", "3", "4"}},
		chatCol{strField("conversation"), []any{"work", "home", "work", "work"}},
	)
	defer rec.Release()
	k, reason := resolveChatColumns(rec.Schema())
	require.Empty(t, reason)
	assert.Equal(t, []string{"work", "home"}, chatConversations(rec, k))

	m, rows, _, _, truncated, _ := foldChat(rec, k, nil, nil, "work", 2)
	require.NoError(t, m.Validate())
	assert.Equal(t, int64(1), truncated)
	assert.Equal(t, []string{"3", "4"}, m.Body)
	assert.Equal(t, []int64{2, 3}, rows)

	m, _, _, _, _, _ = foldChat(rec, k, nil, nil, "home", 0)
	assert.Equal(t, []string{"2"}, m.Body)
}

func TestChatStatusOf(t *testing.T) {
	assert.Equal(t, chatview.StatusRead, chatStatusOf(" Read "))
	assert.Equal(t, chatview.StatusDelivered, chatStatusOf("delivered"))
	assert.Equal(t, chatview.StatusFailed, chatStatusOf("FAILED"))
	assert.Equal(t, chatview.StatusNone, chatStatusOf("seen"))
}

// The driver's fold cache: the same inputs refold nothing; a new conversation
// pick refolds; a stale pick falls back to the first conversation.
func TestChatDriverRebuildCache(t *testing.T) {
	rec := chatRec(t,
		chatCol{chatTsField("ts"), []any{int64(1), int64(2)}},
		chatCol{strField("sender"), []any{"a", "b"}},
		chatCol{strField("body"), []any{"x", "y"}},
		chatCol{strField("conversation"), []any{"one", "two"}},
	)
	defer rec.Release()
	k, reason := resolveChatColumns(rec.Schema())
	require.Empty(t, reason)
	d := NewChatDriver(nil, nil)
	d.conversation = "stale"
	d.rebuild(rec, ResultID(7), k, nil, nil)
	first := d.model
	assert.Equal(t, "one", d.conversation)
	assert.Equal(t, []string{"x"}, first.Body)
	d.rebuild(rec, ResultID(7), k, nil, nil)
	assert.Same(t, first, d.model, "unchanged inputs keep the fold")
	d.conversation = "two"
	d.rebuild(rec, ResultID(7), k, nil, nil)
	assert.NotSame(t, first, d.model)
	assert.Equal(t, []string{"y"}, d.model.Body)
	assert.Equal(t, int32(0), d.ordOf[1])
}
