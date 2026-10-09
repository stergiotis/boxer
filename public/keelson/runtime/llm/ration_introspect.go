package llm

import (
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm/ration"
)

// usageProvider is keelson('llm_usage'): one row per account the ledger has
// charged or refused (ADR-0300 §SD3). A quantity never charged reads 0;
// the token parts a provider did not report are simply not added.
type usageProvider struct{ ledger *ration.Ledger }

func (usageProvider) Name() string                         { return TableUsage }
func (usageProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (usageProvider) Schema() *arrow.Schema                { return usageTable(nil).Schema() }

func (p usageProvider) Snapshot(proj introspect.Projection) (rec arrow.RecordBatch, err error) {
	var rows []ration.AccountUsage
	if p.ledger != nil {
		rows = p.ledger.Accounts()
	}
	rec = usageTable(rows).Build(proj, len(rows))
	return
}

func usageTable(rows []ration.AccountUsage) (t *introspect.Table) {
	total := func(q ration.QuantityE) func(i int) int64 {
		return func(i int) int64 { return rows[i].Totals[q] }
	}
	return introspect.NewTable().
		String("account_kind", func(i int) string { return rows[i].Account.Kind.String() }).
		String("account_key", func(i int) string { return rows[i].Account.Key }).
		Int64("calls", total(ration.QuantityCalls)).
		Int64("input_tokens", total(ration.QuantityInputTokens)).
		Int64("output_tokens", total(ration.QuantityOutputTokens)).
		Int64("total_tokens", total(ration.QuantityTotalTokens)).
		Int64("cached_input_tokens", total(ration.QuantityCachedInputTokens)).
		Int64("reasoning_tokens", total(ration.QuantityReasoningTokens)).
		Int64("wall_ms", total(ration.QuantityWallMs)).
		Int64("inflight", func(i int) int64 { return rows[i].Inflight }).
		Int64("waiting", func(i int) int64 { return int64(rows[i].Waiting) }).
		Int64("reserved_output_tokens", func(i int) int64 { return rows[i].Reserved[ration.QuantityOutputTokens] }).
		Int64("refused", func(i int) int64 { return rows[i].Refused }).
		Int64("queued", func(i int) int64 { return rows[i].Queued }).
		String("last_at", func(i int) string { return formatTime(rows[i].LastAt) })
}

// rationsProvider is keelson('llm_rations'): each rule on each account it
// applies to, with what it has used of its limit.
type rationsProvider struct{ ledger *ration.Ledger }

func (rationsProvider) Name() string                         { return TableRations }
func (rationsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (rationsProvider) Schema() *arrow.Schema                { return rationsTable(nil).Schema() }

func (p rationsProvider) Snapshot(proj introspect.Projection) (rec arrow.RecordBatch, err error) {
	var rows []ration.RuleState
	if p.ledger != nil {
		rows = p.ledger.States()
	}
	rec = rationsTable(rows).Build(proj, len(rows))
	return
}

func rationsTable(rows []ration.RuleState) (t *introspect.Table) {
	return introspect.NewTable().
		String("rule", func(i int) string { return rows[i].Rule.Id }).
		String("kind", func(i int) string { return rows[i].Rule.Kind.String() }).
		String("select", func(i int) string { return rows[i].Rule.Select.String() }).
		String("account_kind", func(i int) string { return rows[i].Account.Kind.String() }).
		String("account_key", func(i int) string { return rows[i].Account.Key }).
		String("quantity", func(i int) string { return string(rows[i].Rule.Quantity) }).
		Int64("used", func(i int) int64 { return rows[i].Used }).
		Int64("limit", func(i int) int64 { return rows[i].Limit }).
		Int64("base_limit", func(i int) int64 { return rows[i].Rule.Limit }).
		Int64("inflight", func(i int) int64 { return rows[i].Inflight }).
		Int64("window_ms", func(i int) int64 { return rows[i].Rule.Window.Milliseconds() }).
		Bool("aligned", func(i int) bool { return rows[i].Rule.Aligned }).
		String("raise_until", func(i int) string { return formatTime(rows[i].Rule.RaiseUntil) }).
		String("author", func(i int) string { return rows[i].Rule.Author }).
		String("reason", func(i int) string { return rows[i].Rule.Reason }).
		String("set_at", func(i int) string { return formatTime(rows[i].Rule.SetAt) })
}

func formatTime(t time.Time) (s string) {
	if !t.IsZero() {
		s = t.UTC().Format(time.RFC3339Nano)
	}
	return
}
