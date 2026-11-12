---
type: explanation
audience: designer of the chat harness (ADR-0265, ADR-0310) and of the moderator (ADR-0302)
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Compiled 2026-11-12 to feed
> [ADR-0370](../adr/0370-stops-declared-conditions-that-hold-or-end-a-conversation-each-a-query-over-its-run.md)
> (proposed); nothing here is a decision. The claims about other products
> were read from each product's public documentation or source on the compile
> date by a model-run research pass and were not re-read by a person. A claim
> marked `[u]` could not be confirmed from a current source; a claim marked
> `[s]` came from a search-result snippet, the page itself not opened. No
> third-party source code was copied. Version-specific names and defaults are
> the kind of fact that ages first.

# Stop criteria in agent harnesses — a survey

The question: what do existing agent harnesses offer for ending or holding a
run on a declared condition, what do they do when the condition holds, and
which of the lessons they learned apply to a harness whose only memory is a
journal?

## What each offers

| Harness | Mechanisms | What happens on trigger |
| --- | --- | --- |
| Claude Code CLI | `--max-turns`, `--max-budget-usd` (print mode); permission modes; hooks on any event returning `continue: false` with a `stopReason`; `PreToolUse` deciding `allow`, `deny`, `ask` or `defer`; `PostToolBatch` and `Stop` hooks deciding `block` | A limit ends the run with a result subtype naming it. `continue: false` halts and overrides the event's own decision. `deny` returns the reason to the model, `ask` prompts the person, `defer` ends the turn for a later resume. A `Stop` hook's `block` is inverted: it keeps the run going |
| Claude Agent SDK | `maxTurns`, `maxBudgetUsd`, `permissionMode`, `canUseTool`, the same hooks | The same result subtypes. `canUseTool` may stay pending, which pauses in place |
| OpenAI Agents SDK | `max_turns` (default `[u]`); input, output and tool guardrails with a tripwire; `tool_use_behavior` including `StopAtTools`; approvals through `needs_approval` and `result.interruptions` | `MaxTurnsExceeded` or a handler's fallback output. A tripwire raises. A tool behaviour returns the tool's output as the answer. An approval pauses into a serialisable run state that a later call resumes |
| Codex CLI | `approval_policy`, `sandbox_mode` | A prompt per action. No turn, time or budget limit was found on the page read |
| LangGraph | `recursion_limit`; `interrupt()` with `Command(resume=…)`; static `interrupt_before` and `interrupt_after` | `GraphRecursionError`. `interrupt()` pauses to the checkpointer, and the node restarts from its beginning on resume |
| LangChain middleware | `ModelCallLimitMiddleware` and `ToolCallLimitMiddleware`, each with a per-run and a per-thread limit and an `exit_behavior`; `HumanInTheLoopMiddleware`; a PII middleware with a regex detector | The model limit ends or raises. The tool limit can also block the one tool and let the model carry on |
| LangChain `AgentExecutor` (legacy) | `max_iterations`, `max_execution_time`, `early_stopping_method` | Either a fixed "stopped" message or one last model call for an answer |
| AutoGen AgentChat | termination conditions: `MaxMessageTermination`, `TextMentionTermination`, `TokenUsageTermination`, `TimeoutTermination`, `HandoffTermination`, `SourceMatchTermination`, `ExternalTermination`, `StopMessageTermination`, `TextMessageTermination`, `FunctionCallTermination`, `FunctionalTermination`; a team `max_turns`; a cancellation token | The run returns with a `stop_reason` in words and the team's state kept. Cancellation raises at once |
| CrewAI | `max_iter`, `max_execution_time`, `max_rpm`; task `human_input` and guardrails | At the iteration limit the agent gives its best answer (mechanism not documented) |
| Vercel AI SDK | `stopWhen` with step-count and `hasToolCall` conditions, a custom predicate over the steps, `prepareStep` (the step-count helper's current name is `[u]`) | The loop returns the steps so far |
| Inspect AI | `message_limit`, `token_limit`, `turn_limit`, `time_limit`, `working_limit`, `cost_limit`, scoped | `LimitExceededError`; the sample ends early and is still scored |
| OpenHands | `max_iterations`, `max_budget_per_task`, `stuck_detection` | The stuck detector can halt the run. Behaviour at the other limits `[u]` |
| Roo Code `[s]` | limits on auto-approved requests and on cost | Pauses before the next call with a "Reset and Continue" prompt |
| Cline `[s]` | per-category auto-approve | Its request limit was removed as of low value |
| Cursor `[s]` | a count of tool calls, then "Continue" | Pause and ask |
| Goose `[s]` | `GOOSE_MAX_TURNS`, `--max-turns` | Stops and prompts |
| smolagents | `max_steps`, `final_answer_checks`, `interrupt()` | At the limit one last model call gives an answer |
| Pydantic AI | `UsageLimits` over requests, tool calls and tokens | `UsageLimitExceeded` |
| Google ADK | `RunConfig.max_llm_calls`, `LoopAgent.max_iterations`, an `exit_loop` tool | The loop exits on escalation |
| Semantic Kernel `AgentGroupChat` (archived) | `MaximumIterations`, `RegexTerminationStrategy`, `KernelFunctionTerminationStrategy`, `AggregatorTerminationStrategy` | Sets a completion flag that must be reset by hand to go on |
| SWE-agent | cost and call limits per instance and in total | `[u]` |
| Aider | a per-call timeout only | — |

## Kinds of criterion

- **Budgets.** Turns or steps in nearly every harness; messages; model calls
  and tool calls counted apart (LangChain, Pydantic AI); tokens; cost; wall
  clock. Inspect AI alone separates wall clock from *working time*, which
  leaves out rate-limit retries, waits on concurrency, tool approval and a
  person's answers.
- **A match on content.** AutoGen's text condition is a substring. A regex
  appears in Semantic Kernel's termination strategy and in LangChain's PII
  detector.
- **A tool.** A named tool was called (AI SDK, OpenAI, AutoGen, ADK), a
  count per tool (LangChain), an approval per call. Matching is by name;
  the arguments are reachable only from custom code.
- **From outside.** A flag another thread sets, a cancellation token.
- **A stuck run.** OpenHands only: fixed patterns over repeated actions,
  errors and monologues.
- **A custom predicate.** Host-language code over the in-memory list of
  steps or messages.
- **A model's judgement.** Claude Code's prompt and agent hooks, Semantic
  Kernel's function strategy, CrewAI's string guardrails.

## What a trigger does

Raise an error; end with a labelled reason; pause and ask; force one last
answer; refuse the one action and continue; hand off. In the harnesses read,
the mechanism fixes the action. LangChain's `exit_behavior` is the one place
a person picks the action per criterion.

## Composition, scope and reset

- AutoGen composes conditions with `|` and `&`; Semantic Kernel has an
  all-or-any aggregator; an AI SDK array is any-of. None offers negation or
  sequence. Claude Code's hooks resolve by a fixed precedence.
- LangChain names the two scopes outright: a limit per run, which is one
  message of the person's, and a limit per thread, which persists.
- AutoGen's conditions reset after each run, and a team's turn count starts
  again on resume while the conversation persists.
- Goose counts turns since the person last spoke.

## Lessons

1. **Where a criterion is checked sets how far a run overshoots it.**
   AutoGen checks once per response; its timeout cannot interrupt a long
   tool call. Claude Code's budget is checked after each response, and its
   documentation says to leave headroom.
2. **Wall clock is not working time.** Inspect AI excludes waits on a person
   and on rate limits, warns that working time can undercount, and advises
   pairing it with a wall-clock limit.
3. **A count that resets must count from the reset.** Roo Code's cost limit
   fired again at once after "Reset and Continue", because cost was still
   summed over the whole history; the fix recorded the message index at the
   reset and counted from there.
4. **Blocking for a person inside a run is fragile without a record.**
   AutoGen warns that a blocked proxy can leave a team in a state that cannot
   be saved. The durable designs stop and resume from stored state.
5. **A text match needs a scope.** AutoGen's and Semantic Kernel's take a
   list of sources. An unscoped match on a sentinel word would also fire on
   the person's own message (an inference, not something a source states).
6. **A limit that fires should say why and leave the run resumable.**
7. **A blunt count that pauses is resented.** Cursor's pause drew forum
   complaints `[s]`, and Cline removed its request limit `[s]`.

## What none of them offers

- **A query over the event log.** Every custom predicate found is code over
  an in-memory list. No harness read exposes a declarative query.
- **Criteria on a work product.** No volume or content criterion on an
  artefact was found.
- **A regex over a tool call's arguments** as a declared criterion.
- **Windowed criteria** such as "N errors in ten minutes", beyond OpenHands'
  fixed patterns.

A journal makes four things cheap that an in-memory list does not: the reset
of lesson 3 as a recorded event, the rows that fired shown to the person as
evidence, a criterion run against past conversations before it is put in
force, and the same statement read afterwards to see why a run stopped.

## Sources

- Claude Code: <https://code.claude.com/docs/en/cli-reference>, <https://code.claude.com/docs/en/hooks>, <https://code.claude.com/docs/en/agent-sdk/agent-loop>, <https://code.claude.com/docs/en/agent-sdk/user-input>
- OpenAI Agents SDK: <https://openai.github.io/openai-agents-python/running_agents/>, <https://openai.github.io/openai-agents-python/guardrails/>, <https://openai.github.io/openai-agents-python/human_in_the_loop/>
- Codex: <https://learn.chatgpt.com/docs/agent-approvals-security>
- LangGraph and LangChain: <https://docs.langchain.com/oss/python/langgraph/graph-api>, <https://docs.langchain.com/oss/python/langgraph/interrupts>, <https://docs.langchain.com/oss/python/langchain/middleware/built-in>, <https://docs.langchain.com/oss/python/langchain/human-in-the-loop>
- AutoGen: <https://microsoft.github.io/autogen/stable/user-guide/agentchat-user-guide/tutorial/termination.html>, <https://microsoft.github.io/autogen/stable/reference/python/autogen_agentchat.conditions.html>, <https://microsoft.github.io/autogen/stable/user-guide/agentchat-user-guide/tutorial/human-in-the-loop.html>
- CrewAI: <https://docs.crewai.com/en/concepts/agents>, <https://docs.crewai.com/en/concepts/tasks>
- Vercel AI SDK: <https://ai-sdk.dev/docs/agents/loop-control>
- Inspect AI: <https://inspect.aisi.org.uk/setting-limits.html>
- OpenHands: <https://docs.openhands.dev/sdk/guides/agent-stuck-detector>
- Roo Code: <https://docs.roocode.com/advanced-usage/rate-limits-costs>, <https://github.com/RooCodeInc/Roo-Code/pull/6890>
- Cline: <https://docs.cline.bot/features/auto-approve>
- Goose: <https://block.github.io/goose/docs/guides/environment-variables>
- smolagents: <https://huggingface.co/docs/smolagents/reference/agents>
- Pydantic AI: <https://pydantic.dev/docs/ai/api/pydantic-ai/usage/>
- Google ADK: <https://adk.dev/runtime/runconfig/>, <https://adk.dev/agents/workflow-agents/loop-agents/>
- Semantic Kernel: <https://learn.microsoft.com/en-us/semantic-kernel/support/archive/agent-chat>
- SWE-agent: <https://swe-agent.com/latest/reference/model_config/>
- Aider: <https://aider.chat/docs/config/options.html>
