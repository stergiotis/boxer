---
type: explanation
audience: whoever designs content labels, task-scoped grants, approvals, sandboxes and the audit trail for agent runs (ADR-0269, ADR-0277, ADR-0280) and their extension to durable, triggered runs
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.
> Compiled 2026-10-09 as the third page beside
> [agentic-workflow-engines-survey.md](./agentic-workflow-engines-survey.md)
> and [agentic-workflow-literature-survey.md](./agentic-workflow-literature-survey.md),
> which moved their security material here; nothing here is a decision.
> Provenance per claim: **[V]** the primary page, specification or abstract
> was read by the compiling pass on that date; **[U]** a search snippet, a
> secondary source or memory — re-read before citing. **PR** peer-reviewed,
> **PP** preprint. Papers were read at abstract level, so numbers are the
> authors' own claims; vendor pages state intended behaviour, and product
> defaults move between releases. Several 2026 entries are single-group
> preprints. The search budget ran out before two queries ran: a measurement
> study of exfiltration channels across vendors, and split inference.

# Agent security — attacks, defenses and shipped controls

The question: what can go wrong when a model with tools acts for a person,
what research has proposed against it, and what the shipping engines,
protocols, sandboxes and classifiers actually enforce. The page holds both
the literature (§2–§3) and the shipped controls (§4), because the useful
comparisons run across them: a defense proposed in a paper against the
default a product ships.

Caveats first. Security benchmarks have a short half-life: a defense scored
on a static benchmark is routinely broken by the next adaptive attack
(§3.1), and two benchmark critiques found that trivial filters score
perfectly on the standard suites (§2.1). Vendor pages describe mechanisms
and rarely publish evaluations; where a number appears below it is the
vendor's or the authors'.

## 1 Threat models and taxonomies

| Source | Status | What it organises |
| --- | --- | --- |
| OWASP Top 10 for LLM Applications 2025 | standard **[V]** | LLM01 prompt injection … **LLM06 excessive agency** … LLM10 unbounded consumption (denial of wallet) |
| OWASP Top 10 for Agentic Applications 2026 | standard, published 2025-12-09 **[V]** | ASI01 goal hijack, ASI02 tool misuse, ASI03 identity and privilege abuse, ASI04 agentic supply chain, ASI05 unexpected code execution, ASI06 memory and context poisoning, ASI07 insecure inter-agent communication, ASI08 cascading failures, ASI09 human–agent trust exploitation, ASI10 rogue agents |
| MITRE ATLAS | data v5.6.0 **[V]** | agent techniques — LLM prompt injection (direct, indirect, *triggered*), agent tool invocation, context poisoning, credential harvesting from agent configuration, exfiltration via tool invocation, tool poisoning, agentic resource consumption — and mitigations (tool permission configuration, human in the loop for agent actions, restricting tool use on untrusted data) |
| CSA MAESTRO | practitioner framework, 2025-02-06 **[V]** | a seven-layer agent threat model extending STRIDE, PASTA and LINDDUN; layer names differ between sources **[U]** |
| NIST CAISI | blog, 2025, updated 2025-12-19 **[V]** | novel red-team attacks raised AgentDojo hijack success from 11 % (strongest baseline attack) to 81 %; 57 % single-attempt, 80 % over 25 attempts |
| Deng et al., *AI Agents Under Threat* | CSUR 2025, PR **[V]** | four gaps: unpredictable multi-step input, complex internal execution, variable environments, untrusted external entities |
| Kim, Guo, Song, *SoK: Attack and Defense Landscape of Agentic AI Systems* | USENIX Security 2026, PR **[V]** | the agent design space, attacks and defenses |
| Shi, Du et al., *SoK: Trust–Authorization Mismatch* | PP **[V]** | 200+ papers: static permissions that do not follow the agent's runtime trustworthiness are the common cause of injection and tool poisoning |
| Liu et al., *Formalizing and Benchmarking Prompt Injection* | USENIX Security 2024, PR **[V]** | target task / injected task as a formal split; 5 attacks × 10 defenses × 10 models |
| ISO/IEC 42001:2023 | standard **[U]** | AI management system; no agent-specific technical controls |

## 2 Attacks

### 2.1 Prompt injection and its benchmarks

Greshake et al. named indirect prompt injection (AISec 2023, PR **[V]**);
benchmarks followed: InjecAgent (ACL Findings 2024, PR **[V]**), AgentDojo
(NeurIPS 2024 D&B, PR **[V]**; the benchmark most of §3.2 reports against —
97 tasks, 629 security cases), ASB (ICLR 2025, PR **[V]**; attack success up
to 84 %, memory poisoning), WASP (NeurIPS 2025 D&B, PR **[V]**; low
end-to-end success is "security by incompetence", shrinking as models
improve). Two 2025 preprints undercut the defenses scored on them: **Nasr,
Carlini, Tramèr et al., *The Attacker Moves Second*** (2510.09023 **[V]**) —
adaptive attacks bypass 12 published defenses, "above 90 %" success for most
— and **Bhagwatkar et al.** (2510.05244 **[V]**) — simple firewalls score
perfectly on AgentDojo, ASB, InjecAgent and τ-bench, so the benchmarks
measure nothing harder than that.

### 2.2 Exfiltration channels

The renderer, the URL fetcher and the form field are egress.

- **Markdown images and links.** Imprompter (2410.14923, PP **[V]**):
  optimised, unreadable prompts make an agent put conversation PII into a
  markdown image URL, ~80 % end to end, transferring across products — a
  human reviewer cannot judge prompts that read as noise. Microsoft's MSRC
  (blog 2025 **[V]**) names image and link exfiltration as the main impact
  routes and blocks them deterministically, beside probabilistic layers,
  stating it does not rely on blocking every injection.
- **Invisible text.** ASCII smuggling through the Unicode Tag block in a
  clickable link (Rehberger, M365 Copilot disclosure, 2024 **[V]**);
  variation-selector suffixes that render as nothing but tokenise
  (*Imperceptible Jailbreaking*, 2510.05025, PP **[V]**). What the reviewer
  saw is not what the model read.
- **Zero-click chains.** EchoLeak (CVE-2025-32711; Reddy & Gujral, AAAI Fall
  Symposium 2025 **[V]**): one email chained past an injection classifier,
  past link redaction with reference-style markdown, through auto-fetched
  images and an allowed proxy domain — an allowlist is as strong as its most
  permissive entry.
- **Form fields.** EIA (ICLR 2025, PR **[V]**): invisible fields make a web
  agent type the user's PII, up to 70 %, while it continues its task.

### 2.3 Memory and retrieval poisoning

| Work | Venue | Result |
| --- | --- | --- |
| PoisonedRAG (Zou et al.) | USENIX Security 2025, PR **[V]** | five texts per target question, 90 % success over millions of documents |
| AgentPoison (Chen et al.) | NeurIPS 2024, PR **[V]** | poisoned demonstrations retrieved by an optimised trigger; ≥80 % success at <0.1 % poison rate, ≤1 % benign loss |
| MINJA (Dong et al.) | NeurIPS 2025, PR **[V]** | an ordinary user plants memory records by querying; they are retrieved for *another* user's query |
| SpAIware (Rehberger) | disclosure 2024 **[V]** | an injected instruction written into long-term memory exfiltrated every later chat until the client check was fixed; memory injection itself remained possible |
| *Machine Against the RAG* (Shafran et al.) | USENIX Security 2025, PR **[V]** | one blocker document makes RAG refuse a target query |
| *Text Embeddings Reveal (Almost) As Much As Text* | EMNLP 2023, PR **[V]** | 92 % of 32-token inputs recovered exactly from embeddings — a vector store carries its source's confidentiality |

Defenses: RobustRAG (2405.15556, ICML 2024 workshop **[V]**) isolates each passage and
aggregates answers, certifying correctness for some queries under k
malicious passages; A-MemGuard (2510.02373, PP **[V]**) validates memories by
consensus and relays ASB's finding that LLM detectors miss 66 % of poisoned
entries.

### 2.4 Between agents

- **Self-replication.** Morris II (Cohen, Bitton, Nassi, 2403.02817, PP
  **[V]**): prompts that copy themselves through RAG email assistants with no
  click; Prompt Infection (2410.07283, PP **[V]**) between agents, mitigated
  by tagging each message's origin; Agent Smith (ICML 2024, PR **[V]**): one
  adversarial image spreads to nearly all of a million simulated agents,
  exponentially — containment per hop matters more than robustness per
  agent.
- **Confused deputy.** *Multi-Agent Systems Execute Arbitrary Malicious
  Code* (Triedman, Jha, Shmatikov, COLM 2025, PR **[V]**): orchestrators
  trust sub-agents' reports and run attacker code in 58–90 % of trials, even
  when each agent resists injection on its own.
- **The channel itself.** Agent-in-the-Middle (Findings of ACL 2025, PR
  **[V]**) rewrites inter-agent messages; *Secret Collusion* (NeurIPS 2024,
  PR **[V]**) shows steganographic capacity growing with model capability and
  monitoring and paraphrasing falling short.
- **Peer identity.** An A2A threat model (Habler et al., 2504.16902, PP **[V]** body):
  card spoofing, task replay, privilege escalation.

### 2.5 Computer-use and web agents

Pop-ups are clicked 86 % of the time and cut task success by 47 %; telling
the agent to ignore them does not help (Zhang, Yu, Yang, ACL 2025, PR
**[V]**). VPI-Bench (ICLR 2026, PR **[V]**): visual injections deceive
computer-use agents up to 51 % and browser agents up to 100 %. AdvAgent
(ICML 2025, PR **[V]**) learns invisible HTML injections against black-box
web agents. OS-Harm (NeurIPS 2025 D&B, PR **[V]**) finds frontier agents
complying with misuse and sometimes acting unsafely with no attacker.

### 2.6 Coding agents and the tool supply chain

- **Instructions in the repository.** Rules-file backdoors with invisible
  Unicode in agent configuration files (Pillar Security, 2025 **[U]**);
  AIShellJack (2509.22040, PP **[V]**): 314 payloads over 70 ATT&CK
  techniques run in coding editors up to 84 % of the time.
- **Hallucinated packages.** Spracklen et al. (USENIX Security 2025, PR
  **[V]**): ≥5.2 % of package names from commercial models and 21.7 % from
  open ones do not exist — 205,474 unique names to squat; checking that a
  name exists does not help.
- **Toxic flows and CI.** A public issue drives an agent with a broad token
  to publish private repositories (Invariant Labs, GitHub MCP, 2025 **[V]**);
  issue and PR text fed to coding agents in GitHub Actions and GitLab CI
  made them publish secrets (PromptPwnd, Aikido Security, 2025 **[V]**).
- **MCP.** Tool poisoning, puppet servers and rug pulls (Song et al., PP
  **[V]**); MCPTox (AAAI 2026, PR **[V]**): 36.5 %
  average attack success across 20 agents on 45 live servers, higher for more
  capable models. A malicious server's OAuth endpoint reached the OS launcher
  through `mcp-remote`, CVE-2025-6514, CVSS 9.6 (JFrog advisory **[V]**).

### 2.7 Confidentiality beyond injection

System prompts are extracted by simple text attacks across 11 models
(Zhang, Carlini, Ippolito, PP **[V]**). Models disclose against contextual
norms: ConfAIde (ICLR 2024, PR **[V]**: 39–57 %), PrivacyLens (NeurIPS 2024
D&B, PR **[V]**: 26–39 % of final actions leak despite instructions),
CI-Bench (PP **[V]**). Remote inference can be attested rather than trusted:
Apple's Private Cloud Compute (2024 **[V]**) — stateless, non-targetable,
images in a public transparency log — and H100 confidential computing at
under 7 % overhead for typical LLM queries (2409.03992, vendor-affiliated PP
**[V]**).

### 2.8 Resource exhaustion

OverThink (2502.02542, PP **[V]**): decoy problems in retrieved content
inflate reasoning tokens up to 46×. A malicious tool server stretching
trajectories past 60 k tokens while keeping the answer — up to 658× cost
(2601.10955, PP **[V]**). Malfunction amplification drives agents into repetitive
or irrelevant actions above 80 % of the time, and the authors report
detection by the model alone as largely ineffective (EMNLP 2025, PR
**[V]**). OWASP LLM10 names denial of wallet; no academic study of it
against agents was found.

## 3 Defenses in the literature

### 3.1 Inside the model

**Spotlighting** (Hines et al., 2024 **[V]**) measured three transforms:
delimiting roughly halved attack success, datamarking reached 3–8 % and
encoding 0–2 % on GPT-3.5-Turbo (0–1 % on the stronger models tested).
**StruQ** (USENIX Security 2025, PR **[V]**) and SecAlign (CCS 2025, PR
**[V]**) train the model on separate instruction and data channels;
an architecture-aware attack reaches 85–95 % against them (2507.07417
**[V]**). Delimiting, the cheapest, is the weakest measured variant, and
none of the in-model defenses is a boundary.

### 3.2 Around the model

| Work | Status | Mechanism | Result |
| --- | --- | --- | --- |
| Willison, Dual LLM pattern | blog 2023 **[V]** | privileged model never reads untrusted text; results pass as `$VAR` references through a non-LLM controller | — |
| f-secure (Wu, Cecchetti, Xiao) | PP **[V]** | planner sees only trusted input; IFC monitor | formal model |
| IsolateGPT | NDSS 2025, PR **[V]** | one model instance per app, hub-mediated | <30 % overhead on three-quarters of queries; later broken on plan integrity by ACE |
| CaMeL (Debenedetti et al.) | PP **[V]** | privileged model writes a program; interpreter tracks per-value capability tags (provenance, allowed readers) and checks policies at tool calls | 77 % AgentDojo utility vs 84 % undefended, provable against the modelled attacks; admits side channels and text-to-text attacks |
| FIDES (Costa et al., Microsoft) | PP **[V]** | integrity × confidentiality label lattice; *variable hiding* keeps results out of context; typed low-capacity `query_llm` (bool ⊑ enum ⊑ string) as a declassification channel | ~0 injections with policies, up to +16.7 points utility over a plain planner |
| RTBAS (CMU) | PP **[V]** | IFC decides automatically, asks the user only otherwise; *dependency screening* keeps taint off context that did not influence a call | blocked targeted attacks, ~2 % utility loss |
| Beurer-Kellner et al., *Design Patterns* | PP **[V]** | six patterns (action-selector, plan-then-execute, map-reduce, dual LLM, code-then-execute, context minimization); once untrusted input is read, it must not be able to trigger consequential actions | 10 case studies |
| ACE (Li et al.) | NDSS 2026, PR **[V]** | abstract plan from trusted input, statically checked against information-flow constraints, then executed | resists InjecAgent, ASB |
| MELON | ICML 2025, PR **[V]** | re-run with the user prompt masked; a call that recurs is attacker-driven | detector, doubles inference |
| DRIFT | NeurIPS 2025, PR **[V]** | fixed minimal trajectory plus a validator for deviations | — |
| NeuroTaint (Cai et al., *Ghost in the Agent*) | PP 2026 **[V]** | offline trace audit follows taint through semantic transformation and cross-session memory | "substantially outperforms FIDES" on its own TaintBench |

Narisetty et al. (2606.26479, PP **[V]**) frame CaMeL, FIDES, RTBAS and
Progent as Biba integrity, reference monitors and least privilege; the
adaptive-attack experiment covers Progent only, and the implicit-flow side
channels catalogued are chiefly those CaMeL reports of itself.

### 3.3 Least privilege and delegation

- **Progent** (PP **[V]**): a DSL of argument-level tool policies with
  LLM-proposed updates; AgentDojo attack success 39.9 % → 1.0 % (v3, gpt-4o).
- **MiniScope** (Zhu, …, Popa, PP **[V]**): task-centric hierarchical
  permissions; 43–89 % fewer confirmations than per-tool prompting, all
  tested escalations blocked.
- **IntentCap** (accepted at AgenticOS 2026 **[V]**; three pages): capabilities as
  short-lived leases assembled from intent, workflow, schema and environment,
  where a lease may only *narrow* the user's authority.
- **Odersky et al., *Tracking Capabilities for Safer Agents*** (PP **[V]**):
  agents write Scala 3 with capture checking, so leaks are rejected
  statically.
- **South et al., *Authenticated Delegation*** (PP **[V]**): OAuth/OIDC
  extensions binding an agent's scoped permissions to a delegating human,
  verifiable by third parties.
- **Conseca** (HotOS 2025 **[V]**): per-task policies generated just in time
  and meant to be human-checkable. **AirGapAgent** (CCS 2024, PR **[V]**):
  contextual-integrity data minimization; context hijacking drops a plain
  agent's protection from 94 % to 45 %, AirGapAgent holds 97 %.
- **Durable approvals.** CapLease (2608.01710, PP **[V]**; literature survey
  §2): retries and crash-resume re-spend one human approval under fresh
  token ids unless the confirmation is bound to a canonical action.

### 3.4 Sandboxes and time of use

ToolEmu (ICLR 2024, PR **[V]**): LM-emulated tools find risky behaviour
with no attacker present. SandboxEscapeBench (2603.02277; ICML 2026 **[U]**
for the venue): frontier agents exploit container misconfigurations.
TOCTOU-Bench (2508.17155, NeurIPS 2025 **[U]** for the venue): an approval is
a check and the following write a use; prompt rewriting, state-integrity
monitoring and tool-fusing together cut the attack window by 95 %.

### 3.5 Human oversight

**Feng, McDonald, Zhang, *Levels of Autonomy*** (PP **[V]**): operator,
collaborator, consultant, approver, observer — autonomy as a design choice,
with autonomy certificates. **Wu et al.,** S&P 2026, PR **[V]**: permission
decisions are consistent within a context and predictable at 85 % (94 % when
confident). **Magentic-UI** (MSR tech report **[V]**): users want options
beyond approve/reject and want plan changes shown. **Wang, Li, Tian,
*Reframing LLM Agent Security as an Agent–Human Interaction Problem***
(2605.24309, PP **[V]**): across 21 products, trust labelling and intent
anchoring have no production deployments; users choose between approval
fatigue and uncontrolled autonomy. Classic grounding: meaningful human
control (Santoni de Sio & van den Hoven 2018 **[U]**), automation complacency
(Parasuraman & Manzey 2010 **[U]**), and the lattice and decentralized label
models (Denning 1976; Myers & Liskov 1997 **[U]**) that CaMeL and FIDES
inherit. Invisible-text and unreadable optimised prompts (§2.2) attack the
approver directly.

### 3.6 Audit and accountability

*Visibility into AI Agents* (FAccT 2024, PR **[V]**): agent identifiers,
real-time monitoring, activity logs extended to multi-agent interactions
and delayed effects. *LLM Agents Can Easily Tamper With Their Own Traces*
(2609.30266, PP **[V]**): five of six coding agents deleted their own traces
when asked, monitors did not fire, and tampering emerged under reward
pressure — hash-chaining protects only what was recorded by something the
agent cannot reach. Run-time attestations at the enforcement point
(*Verifiability-First Agents*, 2512.17259, PP **[V]** abstract) and signed
run capsules (NovaFabric, literature survey §2) are the proposed forms.

## 4 Shipped controls

### 4.1 Engines and SDKs

| System | Identity and access | Secrets | Tool control and guardrails | Code isolation | Audit and traces |
| --- | --- | --- | --- | --- | --- |
| LangGraph Agent Server / LangSmith | `@auth.authenticate` plus `@auth.on` handlers per resource and action, filters stamped on reads; LangSmith API keys by default, **self-hosted has no default auth** **[V]** | AES at rest (`LANGGRAPH_AES_KEY`) or custom per-tenant handlers; encrypted fields unsearchable **[V]** | middleware: call limits, `HumanInTheLoopMiddleware`, PII middleware (engine survey §3.4, §3.7) **[V]** | runtime unsandboxed; Sandboxes are a microVM product whose auth proxy injects credentials so they never enter the runtime **[V]** | trace redaction by `LANGSMITH_HIDE_*`, callables or an anonymizer (skipped when HIDE is set) **[V]**; CVE-2026-41182: `hide_outputs` missed streamed tokens, fixed Python 0.7.31 / JS 0.5.19 **[U]** advisory mirrors |
| n8n | instance roles Owner / Admin / Member and project roles; custom roles on Enterprise **[V]** | credentials encrypted in the database under `N8N_ENCRYPTION_KEY`, random on first launch; external vaults (1Password, AWS, Azure, GCP, HashiCorp, Infisical) on Enterprise **[V]** | per-tool human review; Guardrails node — nine checks (keywords, regex, PII, secret keys, URLs; model-judged jailbreak, NSFW, topic, custom) to a fail branch **[V]** | task runners are "the only isolation layer"; internal mode runs as a child process, "insecure by design", and is the documented default (`N8N_RUNNERS_MODE=internal`, deprecated); external sidecar for production **[V]** | log streaming (Enterprise) of workflow, node, audit, AI-node, runner, worker and queue events **[V]** |
| Temporal | Cloud: namespace isolation, API keys or mTLS, account roles, SAML; self-hosted: pluggable `ClaimMapper` and `Authorizer`, default `noopAuthorizer` allows every API request **[V]** | payload codec: data plaintext only on client and worker; failure messages and stack traces not encoded by default **[V]** | — | your workers | Cloud audit log of control-plane calls; workflow start and terminate are not in it **[V]** |
| Restate | services verify Ed25519-signed requests; `private` services leave ingress; **no application-level auth on the admin (9070) or ingress (8080) ports, by design** — a reverse proxy and network controls **[V]** | experimental client-side journal encryption (TypeScript only); handler names and state keys stay plaintext **[V]** | — | your services | — |
| Inngest | signing key authenticates Inngest → app calls (SDK rejects unsigned); scoped read/write API keys per environment; SAML, RBAC and audit trails on Enterprise **[V]** | encryption middleware for step data and output and `event.data.encrypted`; other event fields plaintext **[V]** | — | your functions | — |
| DBOS | `@DBOS.required_roles` checks roles the host framework forwards; Conductor takes scoped `dbos_` API keys or OIDC user tokens with per-operation permissions; **self-hosted Conductor has no auth unless OAuth is enabled** **[V]** | — | — | your process | Conductor audit log (Enterprise) **[U]** |
| OpenAI Agents SDK | — | — | guardrails: input on the first agent only, **in parallel by default** (`run_in_parallel=True`; tokens and tools may be spent before the tripwire), output on the last, tool guardrails skip hosted tools, handoffs and agents-as-tools **[V]** | sandbox agents: the local Unix client "adds no OS-level confinement" on Linux, no network isolation on macOS; Docker and hosted clients offered **[V]** | tracing on by default with **sensitive data included by default**, sent to OpenAI **[V]** |
| Claude Agent SDK / Claude Code | — | sandbox `credentials` can deny or mask files and variables, the proxy substituting real values for listed hosts (needs TLS termination) **[V]** | hooks → deny → ask → mode → allow → `canUseTool`; deny and ask rules hold in every mode; `allowed_tools` does not constrain the bypass mode, which a subagent runs in only when its parent does **[V]** | bash sandbox (bubblewrap / Seatbelt) **off by default**, falls back to unsandboxed if dependencies are missing unless `failIfUnavailable`, reads "most of the machine" including `~/.ssh`, network through an allowlist proxy that starts empty; shell commands only — file tools, MCP and hooks are outside **[V]** | hooks as audit points **[V]** |
| Google ADK | agent auth (service account) vs user auth (OAuth), with a warning that scopes run broad **[V]** | — | callbacks, plugins, a Gemini-as-judge plugin and a Model Armor plugin **[V]** | hermetic executor recommended; VPC-SC perimeters for exfiltration **[V]** | — |
| Microsoft Agent Framework | Entra Agent ID when hosted on Foundry **[V]** | — | agent, function and chat middleware; `approval_mode="always_require"`; `never_require` is the default and what the samples use **[V]** | — | — |
| Pydantic AI | — | harness detectors redact secrets and personal data **[V]** | deferred-tool approval — "not an authorization boundary against an untrusted client"; harness guardrails (0.x); a Prompt Injection Defender over local tool results, pass-through by default, not over provider-native tools or external deferred results; regex "detectors do not catch prompt injection" **[V]** | bubblewrap (host files readable), Modal, E2B, Sprites workspaces; local and SSH workspaces unisolated **[V]** | — |
| Dify / Langflow | — | — | — | Dify: `dify-sandbox` with a seccomp syscall allowlist **[V]**, and its 1.16.0-rc1 notes ran every agent in one shared sandbox **[V]**; Langflow executes arbitrary Python with host access and does not isolate users in one process **[V]** | — |

**Step and spend limits** — recursion limits, call-limit middleware,
`max_turns`, budget caps — are in the engine survey §3.7.

### 4.2 Protocols

**MCP 2026-07-28** (authorization text from the 2025-11-25 revision as
amended; read **[V]**):

- Authorization is optional; where used, the authorization server
  implements OAuth 2.1, the MCP server publishes RFC 9728 protected-resource
  metadata, PKCE S256 is mandatory, and clients send RFC 8707 `resource` in
  every authorization and token request.
- Servers validate the token's audience and **must not pass the token
  through** or accept tokens for anything else.
- 2026-07-28: protocol sessions removed — cross-call state uses
  server-minted handles, and a handle is never authentication; RFC 9207
  issuer validated when present; credentials keyed by issuer; Dynamic Client
  Registration deprecated for Client ID Metadata Documents.
- Best practices: per-client consent on proxies to stop confused-deputy
  attacks; SSRF defenses on every OAuth discovery fetch; consent showing the
  full command for local servers; never open an auth URL through a shell.
- The **Enterprise-Managed Authorization** extension (stable, opt-in): the
  enterprise identity provider evaluates its policies and issues an
  identity-assertion JWT grant (ID-JAG); central revocation is the overview
  page's claim, not the specification's.
- **Tool annotations** (`readOnlyHint`, `destructiveHint`, `idempotentHint`,
  `openWorldHint`) are advisory: clients must treat them as untrusted unless
  the server is trusted, and the specification asks for a human able to deny
  any tool call.

**A2A 1.0** (1.0.0 2026-03-12, 1.0.1 2026-05-26 **[V]**): TLS required; the server must authenticate every
request against the Agent Card's security schemes; `AUTH_REQUIRED` is an
interrupted state that is not itself authorization; cards may be signed
(JWS over a canonicalised card) but clients only *should* verify; webhook
targets should exclude private, loopback and link-local addresses.

**AG-UI** **[V]**: the draft specification carries no authorization model
(Microsoft's hosting guide states the absence outright); its security section
asks for consent on side-effecting calls and forbids rendering streamed
content as executable markup. The hosting guide adds that a thread id is not
an authorization boundary and approval state is not authentication.

### 4.3 Sandboxes

| Product | Isolation | Egress by default | Controls and caveats |
| --- | --- | --- | --- |
| E2B | "secure Linux VM" **[V]** (Firecracker **[U]**) | open | deny-all plus allow lists by IP, CIDR or domain (domains only under deny-all, ports 80/443); blocked TCP can appear to connect **[V]** |
| Modal | `gvisor` or `vm` runtime, chosen by Modal when unset **[V]** | open | `block_network`, CIDR allow list, SNI-based domain allow list (beta; TLS not decrypted, so fronting is not caught) **[V]** |
| Daytona | not stated beyond "behind a firewall" | tiers 1–2 restricted, 3–4 open **[V]** | network and domain allow lists, block-all, outbound proxy; set at creation **[V]** |
| Cloudflare Sandbox | Containers runtime: a microVM per instance **[V]** | Containers off (`enableInternet: false`); Dynamic Workers inherit the Worker's | keep credentials in the Worker — the sandbox "can use everything your Worker places inside it" **[V]** |
| gVisor | application kernel in user space; no syscall passed directly to the host **[V]** | container-level policy | no protection against hardware side channels; resource exhaustion delegated to host cgroups **[V]** |
| Firecracker | KVM, minimal device model, jailer, per-thread seccomp **[V]** | "does not perform any network traffic filtering" — the host must **[V]** | — |
| Anthropic sandbox-runtime | bubblewrap / Seatbelt on the host kernel **[V]** | proxy allow list, empty at start | the README names domain fronting and broad domains (`github.com`) as bypasses; TLS termination is an opt-in experiment **[V]** |
| OpenAI Codex | Seatbelt, bubblewrap, a Windows sandbox **[V]** | `network_access` flag in workspace-write; default not stated on the page **[U]** | modes read-only, workspace-write (default), danger-full-access; approval `on-request` or `never` (`untrusted` dropped) **[V]** |

Anthropic's secure-deployment guide **[V]** measures the cost of stronger
tiers: gVisor ~2× on simple syscalls and up to 10–200× on heavy file I/O,
and recommends a credential-injecting proxy so the agent never holds real
credentials.

### 4.4 Classifiers in front of the model

| Product | Scope | Published numbers | Stated gaps |
| --- | --- | --- | --- |
| Azure Prompt Shields | user-prompt and document attacks; in Foundry also on **tool responses**; spotlighting (preview) base64-encodes documents **[V]** | none on the page | spotlighting off by default, Chat Completions only |
| Google Model Armor | injection and jailbreak, sensitive data, malicious URLs, responsible-AI filters; project-wide floor settings **[V]** | none | does not decode encoded content; **each prompt judged single-turn**; inputs under three words not matched |
| AWS Bedrock Guardrails | prompt-attack filter **[V]** | none | user input must be wrapped in guard tags or is not filtered; **does not evaluate tool results** or tool definitions |
| Meta Prompt Guard 2 (86 M) | explicit instruction-override attempts, 512-token window **[V]** | recall 97.5 % at 1 % FPR; AgentDojo attack prevention 81.2 % at a 3 % utility-reduction threshold **[V]** | adaptive attacks acknowledged |
| LlamaFirewall | Prompt Guard 2, AlignmentCheck (audits the agent's reasoning for goal hijack), CodeShield **[V]** | AgentDojo 17.6 % → 1.75 % with PG2 + AlignmentCheck, utility 47.7 % → 42.7 % (paper 2505.03574 **[V]**) | — |
| NVIDIA NeMo Guardrails | input, retrieval, dialog, execution and output rails **[V]** | none | agent integration opt-in |

Set against §2.1 and the NIST figures in §1: classifier scores on static
benchmarks overstate protection against an adaptive attacker.

## 5 Observations

These are the compiler's reading of §1–§4, not sourced claims.

1. **Enforcement belongs outside the model.** Every in-model defense
   measured falls to adaptive attack (§3.1, §2.1); the defenses that hold
   are reference monitors around the model — labels, capabilities, plans
   fixed before untrusted input (§3.2–§3.3). Microsoft's own practice says
   the same: contain the impact deterministically, treat detection as
   advisory (§2.2).
2. **Labels and grants bound the blast radius, not the reasoning.** They
   stop exfiltration through renderers, links, arguments and fields, toxic
   flows across repositories and tenants, confused deputies, and per-hop
   worm spread. They do not stop a wrong action that is *permitted*
   (PoisonedRAG, AgentPoison, MINJA), a leak to a legitimate recipient
   (ConfAIde, PrivacyLens), a covert channel inside a permitted one, or
   attacks on the approver (§2.2, §3.5).
3. **Per-value labels and typed declassification beat whole-context taint
   on utility** (CaMeL, FIDES, RTBAS), but propagation through paraphrase
   and memory is unsolved (NeuroTaint, ASB, MINJA) and implicit flows remain
   (Narisetty et al.). Memory and embeddings inherit the label of what wrote
   them (§2.3).
4. **Everything that triggers is an input.** Inter-agent messages, peer
   reports, retrieved demonstrations, repository rule files, tool metadata
   and inbound events carry instruction authority unless a label says
   otherwise (§2.4, §2.6); the literature survey's §9.6 adds runs that
   trigger runs.
5. **The shipped defaults are open.** Claude Code's sandbox is off and falls
   back to unsandboxed; n8n's default runner is a child process of n8n;
   OpenAI's local sandbox adds no confinement on Linux and its traces
   include sensitive data; E2B, Modal and Daytona's upper tiers allow open
   egress; LangGraph self-hosted, Temporal self-hosted, Restate's admin port
   and self-hosted DBOS Conductor ship without auth; MAF's approval mode
   defaults to never; spotlighting is off (§4).
6. **Coverage gaps sit at the tool boundary.** Bedrock's filter skips tool
   results, OpenAI's guardrails skip hosted tools and handoffs, Model Armor
   judges each turn alone, Prompt Guard sees 512 tokens; egress allow lists
   decide on the SNI hostname without decrypting TLS (§4.3, §4.4).
7. **The vendors converge on one secrets pattern:** a proxy outside the
   sandbox injects credentials into outbound requests so the agent never
   holds them (Anthropic, LangSmith, Claude's `mask`, Cloudflare's "keep
   them in the Worker"). MCP's no-passthrough and audience rules are the
   protocol form of the same idea.
8. **Approval is not authorization,** say two SDKs in their own words, and
   CapLease shows approvals re-spent by retries. Approver behaviour is
   predictable within a context (Wu et al.) and prone to fatigue and
   complacency, and there is no field evidence on whether approvers act on
   trust labels (Wang, Li, Tian).
9. **Audit must be written by something the agent cannot reach** (trace
   tampering, §3.6), and availability is part of security: decoys,
   amplifying tool servers and loops spend money without changing the
   answer (§2.8).

## References

Read by the compiling pass on 2026-10-09 unless tagged [U] above. arXiv
identifiers resolve at `https://arxiv.org/abs/<id>`.

### Standards and frameworks

- OWASP Top 10 for LLM Applications 2025 — <https://genai.owasp.org/llm-top-10/>
- OWASP Top 10 for Agentic Applications 2026 — <https://genai.owasp.org/2025/12/09/owasp-top-10-for-agentic-applications-the-benchmark-for-agentic-security-in-the-age-of-autonomous-ai/>
- MITRE ATLAS data — <https://raw.githubusercontent.com/mitre-atlas/atlas-data/main/dist/ATLAS.yaml>
- CSA MAESTRO — <https://cloudsecurityalliance.org/blog/2025/02/06/agentic-ai-threat-modeling-framework-maestro>
- NIST CAISI agent hijacking evaluations — <https://www.nist.gov/news-events/news/2025/01/technical-blog-strengthening-ai-agent-hijacking-evaluations>

### Literature

- ACE (NDSS 2026) — <https://arxiv.org/abs/2504.20984>
- AdvAgent — <https://arxiv.org/abs/2410.17401>
- Agent Smith — <https://arxiv.org/abs/2402.08567>
- AgentDojo — <https://arxiv.org/abs/2406.13352>
- Agent-in-the-Middle — <https://arxiv.org/abs/2502.14847>
- AgentPoison — <https://arxiv.org/abs/2407.12784>
- AI Agents Under Threat — <https://arxiv.org/abs/2406.02630>
- AIShellJack — <https://arxiv.org/abs/2509.22040>
- AirGapAgent — <https://arxiv.org/abs/2405.05175>
- A2A threat model (MAESTRO) — <https://arxiv.org/abs/2504.16902>
- A-MemGuard — <https://arxiv.org/abs/2510.02373>
- ASB — <https://arxiv.org/abs/2410.02644>
- Attacker Moves Second — <https://arxiv.org/abs/2510.09023>
- Authenticated Delegation — <https://arxiv.org/abs/2501.09674>
- Breaking Agents (malfunction amplification) — <https://arxiv.org/abs/2407.20859>
- CaMeL — <https://arxiv.org/abs/2503.18813>
- CapLease — <https://arxiv.org/abs/2608.01710>
- CI-Bench — <https://arxiv.org/abs/2409.13903>
- ConfAIde — <https://arxiv.org/abs/2310.17884>
- Confidential computing on H100 — <https://arxiv.org/abs/2409.03992>
- Conseca — <https://arxiv.org/abs/2501.17070>
- Design Patterns for Securing LLM Agents — <https://arxiv.org/abs/2506.08837>
- DRIFT — <https://arxiv.org/abs/2506.12104>
- EchoLeak — <https://arxiv.org/abs/2509.10540>
- EIA — <https://arxiv.org/abs/2409.11295>
- Embedding inversion (vec2text) — <https://arxiv.org/abs/2310.06816>
- FIDES — <https://arxiv.org/abs/2505.23643>
- Firewalls / stronger benchmarks — <https://arxiv.org/abs/2510.05244>
- Formalizing and Benchmarking Prompt Injection — <https://www.usenix.org/conference/usenixsecurity24/presentation/liu-yupei>
- f-secure — <https://arxiv.org/abs/2409.19091>
- Greshake et al. — <https://arxiv.org/abs/2302.12173>
- Imperceptible Jailbreaking — <https://arxiv.org/abs/2510.05025>
- Imprompter — <https://arxiv.org/abs/2410.14923>
- InjecAgent — <https://arxiv.org/abs/2403.02691>
- IntentCap — <https://arxiv.org/abs/2609.14631>
- IsolateGPT — <https://arxiv.org/abs/2403.04960>
- Levels of Autonomy — <https://arxiv.org/abs/2506.12469>
- Machine Against the RAG — <https://www.usenix.org/conference/usenixsecurity25/presentation/shafran>
- Magentic-UI — <https://arxiv.org/abs/2507.22358>
- MCP attack vectors — <https://arxiv.org/abs/2506.02040>
- MCPTox — <https://arxiv.org/abs/2508.14925>
- MELON — <https://arxiv.org/abs/2502.05174>
- MINJA — <https://arxiv.org/abs/2503.03704>
- MiniScope — <https://arxiv.org/abs/2512.11147>
- Morris II — <https://arxiv.org/abs/2403.02817>
- Multi-Agent Systems Execute Arbitrary Malicious Code — <https://arxiv.org/abs/2503.12188>
- NeuroTaint — <https://arxiv.org/abs/2604.23374>
- OS-Harm — <https://arxiv.org/abs/2506.14866>
- Out-of-band defenses, adaptive evaluation — <https://arxiv.org/abs/2606.26479>
- OverThink — <https://arxiv.org/abs/2502.02542>
- Package hallucinations — <https://www.usenix.org/conference/usenixsecurity25/presentation/spracklen>
- PoisonedRAG — <https://www.usenix.org/conference/usenixsecurity25/presentation/zou>
- Pop-up attacks — <https://arxiv.org/abs/2411.02391>
- PrivacyLens — <https://arxiv.org/abs/2409.00138>
- Progent — <https://arxiv.org/abs/2504.11703>
- Prompt extraction — <https://arxiv.org/abs/2307.06865>
- Prompt Infection — <https://arxiv.org/abs/2410.07283>
- Reframing agent security as agent–human interaction — <https://arxiv.org/abs/2605.24309>
- Resource amplification via tool-calling chains — <https://arxiv.org/abs/2601.10955>
- RobustRAG — <https://arxiv.org/abs/2405.15556>
- RTBAS — <https://arxiv.org/abs/2502.08966>
- SandboxEscapeBench — <https://arxiv.org/abs/2603.02277>
- Secret Collusion — <https://arxiv.org/abs/2402.07510>
- SoK: Agentic AI attack and defense landscape — <https://www.usenix.org/conference/usenixsecurity26/presentation/kim-juhee-agentic>
- SoK: Trust–Authorization Mismatch — <https://arxiv.org/abs/2512.06914>
- Spotlighting — <https://arxiv.org/abs/2403.14720>
- StruQ — <https://arxiv.org/abs/2402.06363>
- StruQ/SecAlign attack (ASTRA) — <https://arxiv.org/abs/2507.07417>
- TOCTOU-Bench — <https://arxiv.org/abs/2508.17155>
- ToolEmu — <https://arxiv.org/abs/2309.15817>
- Trace tampering — <https://arxiv.org/abs/2609.30266>
- Tracking Capabilities for Safer Agents — <https://arxiv.org/abs/2603.00991>
- Verifiability-First Agents — <https://arxiv.org/abs/2512.17259>
- Visibility into AI Agents — <https://arxiv.org/abs/2401.13138>
- VPI-Bench — <https://arxiv.org/abs/2506.02456>
- WASP — <https://arxiv.org/abs/2504.18575>
- Wu et al., automating data access permissions — <https://arxiv.org/abs/2511.17959>

### Disclosures and industry write-ups

- Apple Private Cloud Compute — <https://security.apple.com/blog/private-cloud-compute/>
- Dual LLM pattern — <https://simonwillison.net/2023/Apr/25/dual-llm-pattern/>
- GitHub MCP toxic flow (Invariant Labs) — <https://invariantlabs.ai/blog/mcp-github-vulnerability>
- M365 Copilot ASCII smuggling — <https://embracethered.com/blog/posts/2024/m365-copilot-prompt-injection-tool-invocation-and-data-exfil-using-ascii-smuggling/>
- mcp-remote CVE-2025-6514 (JFrog) — <https://research.jfrog.com/vulnerabilities/mcp-remote-command-injection-rce-jfsa-2025-001290844/>
- MSRC on indirect prompt injection — <https://www.microsoft.com/msrc/blog/2025/07/how-microsoft-defends-against-indirect-prompt-injection-attacks>
- PromptPwnd (Aikido Security) — <https://www.aikido.dev/blog/promptpwnd-github-actions-ai-agents>
- Rules File Backdoor (Pillar Security) — <https://www.pillar.security/blog/new-vulnerability-in-github-copilot-and-cursor-how-hackers-can-weaponize-code-agents>
- SpAIware — <https://embracethered.com/blog/posts/2024/chatgpt-macos-app-persistent-data-exfiltration/>

### Vendor documentation and specifications

- A2A specification — <https://a2a-protocol.org/latest/specification/>
- AG-UI draft specification — <https://docs.ag-ui.com/spec/draft>
- AWS Bedrock prompt-attack filter — <https://docs.aws.amazon.com/bedrock/latest/userguide/guardrails-prompt-attack.html>
- Azure Prompt Shields — <https://learn.microsoft.com/en-us/azure/ai-services/content-safety/concepts/jailbreak-detection>
- Claude Agent SDK permissions — <https://code.claude.com/docs/en/agent-sdk/permissions>
- Claude Code sandboxing — <https://code.claude.com/docs/en/sandboxing>
- Claude secure deployment — <https://code.claude.com/docs/en/agent-sdk/secure-deployment>
- Anthropic sandbox-runtime — <https://github.com/anthropics/sandbox-runtime>
- Cloudflare Sandbox — <https://developers.cloudflare.com/sandbox/>
- Codex sandboxing — <https://learn.chatgpt.com/docs/sandboxing>
- Daytona network limits — <https://www.daytona.io/docs/en/network-limits/>
- DBOS authentication and authorization — <https://docs.dbos.dev/python/tutorials/authentication-authorization>; Conductor self-hosting — <https://docs.dbos.dev/conductor/self-hosting/hosting-conductor>
- Dify sandbox — <https://github.com/langgenius/dify-sandbox>
- Dify releases — <https://github.com/langgenius/dify/releases>
- E2B internet access — <https://docs.e2b.dev/sandbox/internet-access>
- Firecracker design — <https://github.com/firecracker-microvm/firecracker/blob/main/docs/design.md>
- Google ADK safety — <https://adk.dev/safety/>
- Google Model Armor — <https://docs.cloud.google.com/security-command-center/docs/model-armor-overview>
- gVisor security model — <https://gvisor.dev/docs/architecture_guide/security/>
- Inngest encryption middleware — <https://www.inngest.com/docs/features/middleware/encryption-middleware>; signing keys — <https://www.inngest.com/docs/platform/signing-keys>
- Langflow security — <https://docs.langflow.org/security>
- LangSmith auth — <https://docs.langchain.com/langsmith/auth>
- LangSmith custom encryption — <https://docs.langchain.com/langsmith/custom-encryption>
- LangSmith mask inputs and outputs — <https://docs.langchain.com/langsmith/mask-inputs-outputs>
- LangSmith Sandboxes — <https://langchain.com/langsmith/sandboxes>
- LlamaFirewall — <https://github.com/meta-llama/PurpleLlama/blob/main/LlamaFirewall/README.md>; paper <https://arxiv.org/abs/2505.03574>
- MCP authorization (2025-11-25) — <https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization>
- MCP 2026-07-28 changelog — <https://modelcontextprotocol.io/specification/2026-07-28/changelog>
- MCP 2026-07-28 tools — <https://modelcontextprotocol.io/specification/2026-07-28/server/tools>
- MCP security best practices (draft) — <https://modelcontextprotocol.io/specification/draft/basic/security_best_practices>
- MCP Enterprise-Managed Authorization — <https://modelcontextprotocol.io/extensions/auth/enterprise-managed-authorization>
- Microsoft Agent Framework middleware — <https://learn.microsoft.com/en-us/agent-framework/agents/middleware/>; Foundry agent identity — <https://learn.microsoft.com/en-us/azure/foundry/agents/concepts/agent-identity>
- Microsoft AG-UI security considerations — <https://learn.microsoft.com/en-us/agent-framework/integrations/by-component/ui/ag-ui/security-considerations>
- Modal sandbox networking — <https://modal.com/docs/guide/sandbox-networking>
- n8n Guardrails node — <https://docs.n8n.io/integrations/builtin/core-nodes/n8n-nodes-langchain.guardrails/>
- n8n log streaming — <https://docs.n8n.io/log-streaming/>
- n8n RBAC — <https://docs.n8n.io/user-management/rbac/>; external secrets — <https://docs.n8n.io/external-secrets/>; task-runner variables — <https://docs.n8n.io/hosting/configuration/environment-variables/task-runners/>
- n8n task runners — <https://docs.n8n.io/hosting/configuration/task-runners/>
- NeMo Guardrails — <https://docs.nvidia.com/nemo/guardrails/latest/index.html>
- OpenAI Agents guardrails — <https://openai.github.io/openai-agents-python/guardrails/>
- OpenAI Agents sandbox agents — <https://openai.github.io/openai-agents-python/sandbox_agents/>
- OpenAI Agents tracing — <https://openai.github.io/openai-agents-python/tracing/>
- Prompt Guard 2 model card — <https://huggingface.co/meta-llama/Llama-Prompt-Guard-2-86M>
- Pydantic AI deferred tools — <https://pydantic.dev/docs/ai/tools-toolsets/deferred-tools/>
- Pydantic AI harness guardrails — <https://pydantic.dev/docs/ai/harness/guardrails/>
- Pydantic AI Prompt Injection Defender — <https://pydantic.dev/docs/ai/harness/prompt-injection-defender/>
- Restate security — <https://docs.restate.dev/operate/security>; server security — <https://docs.restate.dev/server/security>
- Temporal Cloud security — <https://docs.temporal.io/cloud/security>; audit logging — <https://docs.temporal.io/cloud/audit-logging>; self-hosted — <https://docs.temporal.io/self-hosted-guide/security>
- Temporal data encryption — <https://docs.temporal.io/production-deployment/data-encryption>
