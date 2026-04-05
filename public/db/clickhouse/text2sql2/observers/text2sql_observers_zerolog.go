//go:build llm_generated_opus46

package observers

import (
	"context"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/db/clickhouse/text2sql2/orchestrator"
)

// ZerologObserver implements ObserverI using zerolog structured logging.
// Each pipeline stage emits a log event at the appropriate level with
// typed fields matching the event data.
//
// Usage:
//
//	logger := zerolog.New(os.Stderr).With().Timestamp().Logger()
//	observer := NewZerologObserver(logger)
//	orch := orchestrator.New(cfg, llm, ch, cache, observer)
type ZerologObserver struct {
	logger zerolog.Logger
}

var _ orchestrator.ObserverI = (*ZerologObserver)(nil)

// NewZerologObserver creates an observer that logs to the given logger.
func NewZerologObserver(logger zerolog.Logger) *ZerologObserver {
	return &ZerologObserver{logger: logger}
}

func (inst *ZerologObserver) OnTemplateParsed(_ context.Context, e orchestrator.TemplateParsedEvent) {
	ev := inst.logger.Info().
		Str("stage", "template_parsed").
		Dur("duration", e.Duration).
		Str("model", e.Template.Model).
		Int("params", len(e.Template.Params)).
		Int("examples", len(e.Template.Examples)).
		Int("joins", len(e.Template.Joins)).
		Bool("pinned", e.Template.Pinned)
	if e.Err != nil {
		ev = ev.AnErr("err", e.Err)
	}
	ev.Msg("template parsed")
}

func (inst *ZerologObserver) OnCacheHit(_ context.Context, e orchestrator.CacheHitEvent) {
	inst.logger.Info().
		Str("stage", "cache_hit").
		Str("cache_key", e.CacheKey).
		Bool("pinned", e.Entry.Pinned).
		Time("compiled_at", e.Entry.CompiledAt).
		Msg("cache hit")
}

func (inst *ZerologObserver) OnCacheMiss(_ context.Context, e orchestrator.CacheMissEvent) {
	inst.logger.Info().
		Str("stage", "cache_miss").
		Str("cache_key", e.CacheKey).
		Msg("cache miss")
}

func (inst *ZerologObserver) OnLLMRequest(_ context.Context, e orchestrator.LLMRequestEvent) {
	inst.logger.Debug().
		Str("stage", "llm_request").
		Str("model", e.Model).
		Int("attempt", e.Attempt).
		Int("messages", len(e.Messages)).
		Msg("LLM request")
}

func (inst *ZerologObserver) OnLLMResponse(_ context.Context, e orchestrator.LLMResponseEvent) {
	ev := inst.logger.Info().
		Str("stage", "llm_response").
		Str("model", e.Model).
		Int("attempt", e.Attempt).
		Dur("duration", e.Duration).
		Int("response_len", len(e.RawResponse)).
		Int("sql_len", len(e.ExtractedSQL))
	if e.Err != nil {
		ev = ev.AnErr("err", e.Err)
	}
	ev.Msg("LLM response")
}

func (inst *ZerologObserver) OnGrammar1Parse(_ context.Context, e orchestrator.Grammar1ParseEvent) {
	ev := inst.logger.Log().
		Str("stage", "grammar1_parse").
		Int("attempt", e.Attempt).
		Dur("duration", e.Duration).
		Int("sql_len", len(e.SQL))
	if e.Err != nil {
		ev.AnErr("err", e.Err).Msg("grammar1 parse failed")
	} else {
		ev.Msg("grammar1 parse ok")
	}
}

func (inst *ZerologObserver) OnNormalize(_ context.Context, e orchestrator.NormalizeEvent) {
	ev := inst.logger.Log().
		Str("stage", "normalize").
		Int("attempt", e.Attempt).
		Dur("duration", e.Duration).
		Int("input_len", len(e.InputSQL)).
		Int("output_len", len(e.OutputSQL))
	if e.Err != nil {
		ev.AnErr("err", e.Err).Msg("normalize failed")
	} else {
		ev.Msg("normalize ok")
	}
}

func (inst *ZerologObserver) OnGrammar2Parse(_ context.Context, e orchestrator.Grammar2ParseEvent) {
	ev := inst.logger.Log().
		Str("stage", "grammar2_parse").
		Int("attempt", e.Attempt).
		Dur("duration", e.Duration).
		Int("sql_len", len(e.SQL))
	if e.Err != nil {
		ev.AnErr("err", e.Err).Msg("grammar2 parse failed")
	} else {
		ev.Msg("grammar2 parse ok")
	}
}

func (inst *ZerologObserver) OnASTConvert(_ context.Context, e orchestrator.ASTConvertEvent) {
	ev := inst.logger.Log().
		Str("stage", "ast_convert").
		Int("attempt", e.Attempt).
		Dur("duration", e.Duration)
	if e.Err != nil {
		ev.AnErr("err", e.Err).Msg("AST convert failed")
	} else {
		ev.Msg("AST convert ok")
	}
}

func (inst *ZerologObserver) OnPolicyEnforce(_ context.Context, e orchestrator.PolicyEnforceEvent) {
	ev := inst.logger.Info().
		Str("stage", "policy_enforce").
		Dur("duration", e.Duration).
		Int("input_len", len(e.InputSQL)).
		Int("output_len", len(e.OutputSQL)).
		Bool("modified", e.InputSQL != e.OutputSQL)
	if e.Err != nil {
		ev.AnErr("err", e.Err).Msg("policy enforce failed")
	} else {
		ev.Msg("policy enforce ok")
	}
}

func (inst *ZerologObserver) OnExecute(_ context.Context, e orchestrator.ExecuteEvent) {
	ev := inst.logger.Info().
		Str("stage", "execute").
		Dur("duration", e.Duration).
		Int("sql_len", len(e.SQL)).
		Int("params", len(e.Params))
	if e.Result != nil {
		ev = ev.
			Int64("rows", e.Result.RowCount).
			Int64("data_size", e.Result.DataSize).
			Str("format", e.Result.Format).
			Dur("ch_elapsed", e.Result.Elapsed)
	}
	if e.Err != nil {
		ev.AnErr("err", e.Err).Msg("execute failed")
	} else {
		ev.Msg("execute ok")
	}
}

func (inst *ZerologObserver) OnResultAnalysis(_ context.Context, e orchestrator.ResultAnalysisEvent) {
	ev := inst.logger.Info().
		Str("stage", "result_analysis").
		Dur("duration", e.Duration).
		Str("verdict", e.LLMVerdict)
	if e.Err != nil {
		ev.AnErr("err", e.Err).Msg("result analysis failed")
	} else {
		ev.Msg("result analysis ok")
	}
}

func (inst *ZerologObserver) OnComplete(_ context.Context, e orchestrator.CompleteEvent) {
	ev := inst.logger.Info().
		Str("stage", "complete").
		Str("cache_key", e.CacheKey).
		Dur("total_duration", e.TotalDuration).
		Int("attempts", e.Attempts).
		Bool("from_cache", e.FromCache)
	if e.Err != nil {
		ev.AnErr("err", e.Err).Msg("pipeline failed")
	} else {
		ev.Int("sql_len", len(e.FinalSQL)).Msg("pipeline ok")
	}
}
