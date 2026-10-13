package attention

import (
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// TableQueue is keelson('attention_queue'): the decisions the person owes,
// one row per item the queue holds, open and shelved (ADR-0324, update of
// 2026-10-13).
const TableQueue = "attention_queue"

// PendingI is the queue as a read side.
type PendingI interface {
	Pending(now time.Time) (entries []Entry)
}

// RegisterIntrospect exposes the queue as keelson('attention_queue'). A nil
// queue registers an empty table, so the table name does not depend on
// whether the host built one.
func RegisterIntrospect(reg *introspect.Registry, q PendingI) (err error) {
	return reg.Register(queueProvider{q: q})
}

type queueProvider struct{ q PendingI }

func (queueProvider) Name() string                         { return TableQueue }
func (queueProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (queueProvider) Schema() *arrow.Schema                { return queueTable(nil).Schema() }
func (inst queueProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	var rows []Entry
	if inst.q != nil {
		rows = inst.q.Pending(time.Now())
	}
	return queueTable(rows).Build(proj, len(rows)), nil
}

func unixMilliOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

func queueTable(rows []Entry) *introspect.Table {
	r := func(i int) *Entry { return &rows[i] }
	return introspect.NewTable().
		String("key", func(i int) string { return r(i).Key }).
		// grant.request, grant.widening, confirmation, moderator.question,
		// question, exit.approval or urgency.
		String("kind", func(i int) string { return r(i).Kind }).
		String("level", func(i int) string { return r(i).Level.String() }).
		String("effect", func(i int) string { return r(i).Effect.String() }).
		String("app", func(i int) string { return string(r(i).App) }).
		Uint64("instance", func(i int) uint64 { return r(i).Instance }).
		String("conversation", func(i int) string { return r(i).Conversation }).
		String("summary", func(i int) string { return r(i).Summary }).
		Int64("created_unix_ms", func(i int) int64 { return unixMilliOrZero(r(i).Created) }).
		// The earliest clock that ends the item and what it is; zero for
		// none.
		Int64("bound_unix_ms", func(i int) int64 { return unixMilliOrZero(r(i).Bound) }).
		String("bound_why", func(i int) string { return r(i).BoundWhy }).
		Bool("acked", func(i int) bool { return r(i).Acked }).
		Int64("shelved_until_unix_ms", func(i int) int64 { return unixMilliOrZero(r(i).ShelvedUntil) }).
		Bool("escalated", func(i int) bool { return r(i).Escalated }).
		Bool("near_bound", func(i int) bool { return r(i).NearBound })
}
