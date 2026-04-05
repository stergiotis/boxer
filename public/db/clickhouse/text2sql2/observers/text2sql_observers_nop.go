//go:build llm_generated_opus46

package observers

import (
	"context"

	"github.com/stergiotis/boxer/public/db/clickhouse/text2sql2/orchestrator"
)

// ============================================================================
// NopObserver — default no-op implementation
// ============================================================================

// NopObserver is a no-op ObserverI that discards all events.
// Embed it to implement only the methods you care about.
type NopObserver struct{}

var _ orchestrator.ObserverI = NopObserver{}

func (NopObserver) OnTemplateParsed(context.Context, orchestrator.TemplateParsedEvent) {}
func (NopObserver) OnCacheHit(context.Context, orchestrator.CacheHitEvent)             {}
func (NopObserver) OnCacheMiss(context.Context, orchestrator.CacheMissEvent)           {}
func (NopObserver) OnLLMRequest(context.Context, orchestrator.LLMRequestEvent)         {}
func (NopObserver) OnLLMResponse(context.Context, orchestrator.LLMResponseEvent)       {}
func (NopObserver) OnGrammar1Parse(context.Context, orchestrator.Grammar1ParseEvent)   {}
func (NopObserver) OnNormalize(context.Context, orchestrator.NormalizeEvent)           {}
func (NopObserver) OnGrammar2Parse(context.Context, orchestrator.Grammar2ParseEvent)   {}
func (NopObserver) OnASTConvert(context.Context, orchestrator.ASTConvertEvent)         {}
func (NopObserver) OnPolicyEnforce(context.Context, orchestrator.PolicyEnforceEvent)   {}
func (NopObserver) OnExecute(context.Context, orchestrator.ExecuteEvent)               {}
func (NopObserver) OnResultAnalysis(context.Context, orchestrator.ResultAnalysisEvent) {}
func (NopObserver) OnComplete(context.Context, orchestrator.CompleteEvent)             {}
