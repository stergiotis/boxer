---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Compiled 2026-10-09 as background to
> [ADR-0300](../adr/0300-metered-model-calls-and-rules-a-moderator-writes.md)
> (proposed). Nothing here is a decision. The ADR is authoritative; this page
> is the survey it leans on.
>
> **Provenance.** Product documentation and vendor blog posts read on
> 2026-10-09, linked in §4. Vendor posts comparing their own product with
> others are marked as such. Nothing was installed or measured.

# LLM gateways: what they meter and how they limit

The question behind this page: what do LLM proxies and gateways commonly
offer for attributing, metering and limiting model use, and which of it
fits a host service that already sits between every app and the model
([ADR-0254](../adr/0254-model-inference-as-a-keelson-capability.md)).

## 1. Common features

| Concern | What gateways do | Examples |
|---|---|---|
| Attribution | A virtual key per caller, plus labels (user, team, customer, session) | LiteLLM keys, teams, end-users; Bifrost customer → team → virtual key; Portkey `_user` metadata; Helicone segments by custom property |
| Hierarchy | A request must pass the budget at every level of its chain | Bifrost checks each level; LiteLLM checks key and user separately |
| Budget windows | Several windows per scope (e.g. 24 h and 30 d), rolling or aligned to the calendar; per-model budgets | LiteLLM `budget_limits[]`, `model_max_budget`; Bifrost `budgets[]` with `reset_duration`; Cloudflare fixed or rolling |
| Temporary raises | Extra allowance with an expiry, leaving the base limit unchanged | Bifrost `override_amount` for N cycles; LiteLLM `temp_budget_increase` with expiry |
| Rate limits | Tokens and requests per window; calls in flight | LiteLLM `tpm_limit`, `rpm_limit`, `max_parallel_requests`, `token_rate_limit_type` (input, output, total) |
| Weighted cost | One expression over token types | Envoy AI Gateway `llmRequestCosts` with a CEL expression, e.g. cached input at 0.1×, output at 1.5× |
| Agent sessions | Caps on calls and spend per agent session, keyed by a trace id | LiteLLM `max_iterations`, `max_budget_per_session` |
| Overshoot | Either charge after the response, so the call that crosses the limit completes, or reserve an estimate before and settle after | Envoy and Cloudflare charge after; LiteLLM reserves from `max_tokens` by default |
| Refusals | Typed so the caller can tell wait from stop | Bifrost 402 `budget_exceeded`, 429 `token_limited` / `request_limited`, 403 `model_blocked` |
| Remaining quota | Returned with each reply | LiteLLM `x-litellm-key-remaining-tokens-*`; Helicone `RateLimit-Remaining` after the IETF draft |
| Policy hooks | Code before, during and after the call; can change, refuse or answer the call | LiteLLM `async_pre_call_hook` (a returned string becomes the assistant reply); Bifrost `PreHook` short-circuit; Portkey guardrail webhooks with `deny` |
| Instead of refusing | Fall back to a cheaper model when a budget runs out | LiteLLM fallbacks on per-model limits; Cloudflare |
| Early warning | Soft thresholds that alert before the hard cap | LiteLLM soft budgets; Portkey `alert_threshold` |
| Counter store down | Configurable allow or refuse | LiteLLM `fail_closed_budget_enforcement`; Portkey `failOnError`, which defaults to allowing |
| Management | An admin API for keys, budgets and limits | LiteLLM `/key/update`; OpenRouter Management API; Bifrost governance API |
| Telemetry | OTel GenAI `gen_ai.client.token.usage` with `gen_ai.token.type` | Kong and others. The conventions were marked Development in 2026. |

## 2. What follows for a host service

- **Identity.** Gateways issue keys because they cannot see the caller.
  `runtime.llm` gets the caller from the bus envelope (`app.Msg.Sender`,
  `SenderInstance`) and the agent task from the delegation context, so the
  instance plays the part of a key and the task the part of a session id.
- **Counters.** Gateways keep counters in Redis to share them across
  replicas, which is why most charge after the response: an exact
  reservation across replicas is expensive. Counters in one process make the
  reservation cheap.
- **Synchronous judges.** Portkey's guardrail webhook is the "ask an outside
  judge on every call" design. Its defaults, a 3 s timeout and letting the
  call through when the judge fails, show what that design costs. Every
  gateway surveyed checks its limits against a stored rule table, with
  anything slower behind an admin API.
- **Dollars.** Gateways price calls per model. A local model has no price,
  and LiteLLM skips budgets for models priced at zero.

## 3. Energy and device time

Recorded because the question came up; the ADR defers it.

- No hosted or self-hosted LLM API returned energy per request as of
  2026-10-09, and no standard defines such a field. llama.cpp returns a
  `timings` object (prompt and generation counts and milliseconds) on
  `/v1/chat/completions`; Ollama's native API returns load, prompt and
  generation durations. vLLM and SGLang expose aggregate Prometheus metrics.
- The OTel GenAI conventions carry no energy attribute. The Green Software
  Foundation's `sci-otel` vocabulary was pre-draft; *SCI for AI* is a
  calculation method, not a wire format.
- Published research measures energy outside the server, by sampling device
  power (NVML, DCGM) and integrating over time. Estimators such as EcoLogits
  multiply tokens by a per-model coefficient.
- If energy has to come from the API, the remaining options are a metering
  proxy on the GPU host that adds a field to each reply, estimates labelled
  as such, or a field added to a server upstream.

## 4. Sources

- [LiteLLM — budgets and rate limits](https://docs.litellm.ai/docs/proxy/users)
- [LiteLLM — call hooks](https://docs.litellm.ai/docs/proxy/call_hooks)
- [LiteLLM — agent iteration budgets](https://docs.litellm.ai/docs/a2a_iteration_budgets)
- [Bifrost — virtual keys and governance](https://docs.getbifrost.ai/features/governance/virtual-keys)
- [Bifrost — writing plugins](https://docs.getbifrost.ai/plugins/writing-plugin)
- [Envoy AI Gateway — usage-based rate limiting](https://theagentrouter.ai/docs/capabilities/traffic/usage-based-ratelimiting)
- [agentgateway — rate limits](https://agentgateway.dev/docs/configuration/resiliency/rate-limits)
- [Portkey — budget limits](https://portkey.ai/docs/product/ai-gateway/virtual-keys/budget-limits.md)
- [Portkey — rate limits](https://portkey.ai/docs/product/ai-gateway/virtual-keys/rate-limits)
- [Portkey — metadata](https://portkey.ai/docs/product/observability/metadata)
- [Portkey — guardrails](https://docs.portkey.ai/docs/product/guardrails)
- [Helicone — custom rate limits](https://docs.helicone.ai/features/advanced-usage/custom-rate-limits)
- [OpenRouter — spend controls](https://openrouter.ai/docs/guides/best-practices/spend-controls)
- [Cloudflare AI Gateway — spend limits](https://developers.cloudflare.com/ai-gateway/features/spend-limits)
- [OTel GenAI metrics](https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-metrics/)
- [Kong — GenAI OTel metrics reference](https://developer.konghq.com/ai-gateway/ai-otel-metrics/)
- [Gateway comparison (vendor-authored)](https://getmaxim.ai/articles/top-5-open-source-llm-gateways-compared-2026/)
- [arXiv 2502.05610 — API providers report no energy metrics](https://arxiv.org/pdf/2502.05610)
- [llama.cpp issue 12968 — `timings` in chat completions](https://github.com/ggml-org/llama.cpp/issues/12968)
- [GSF sci-otel](https://github.com/Green-Software-Foundation/sci-otel)
- [SCI for AI](https://sci-for-ai.greensoftware.foundation/)
