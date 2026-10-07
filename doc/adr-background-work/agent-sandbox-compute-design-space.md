---
type: explanation
audience: package maintainer
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-09-27
---

> **Provenance.** Compiled 2026-09-27. Nothing here is a decision, and no
> boxer work is proposed. This is a clean-room survey. It
> rests on papers, product documentation, engineering blogs, security
> advisories and project design documents read on that date, partly through
> delegated passes that opened no implementation source of any surveyed
> project. Product documentation describes products as their makers see them.
> Claims marked **[2nd]** come from a third-party write-up or a search snippet
> that was not checked against its primary source. Most pages were read
> through a fetch tool that summarises, so check the wording at the link
> before quoting it. Statements about boxer (§9) were checked against the
> working tree on the same date. The page gives orders of magnitude rather
> than vendor figures; the sources carry the figures and their conditions.

# Sandboxed, agent-driven compute: requirements, competences and design space

## 1 Scope

Between function-as-a-service, dev containers and CI runners, a category of
infrastructure has formed. It hands an LLM agent, or the harness driving one,
an isolated and usually stateful execution environment on demand, and routes
the agent's tool calls into it. This page asks:

1. What does this workload require of infrastructure, and for whom?
2. Which competences does a platform need to meet those requirements?
3. How do existing systems divide the design space, and where are the gaps?
4. What part do software minimalism and a small attack surface play, and how
   much of the usual argument for them is supported by evidence?
5. How is performance isolated, both against denial of service and for
   measurements that can be trusted?

Two sources opened the question and recur throughout. The DSec paper from
DeepSeek-AI (arXiv 2609.22978) describes sandbox infrastructure for
reinforcement-learning training at production scale. The Agent Substrate
project (github.com/agent-substrate/substrate, pre-1.0, documentation read
only) is a runtime for many long-lived, mostly idle agents. They sit at
opposite ends of the category.

The agent frameworks themselves, model serving, and whether boxer should build
or adopt any of this are out of scope.

## 2 The category and its workloads

### 2.1 What sets it apart

Classic function-as-a-service runs short, stateless, interchangeable
invocations of code the tenant trusts. Agent sandboxes run the opposite:
long-lived, stateful, individually named instances that sit idle most of the
time and execute code nobody vouches for. The Berkeley paper on serverless
computing (Jonas et al., arXiv 1902.03383, 2019) listed what held serverless
back: fine-grained state, coordination between named and directly addressable
instances, predictable start latency, and a clean starting state for each
invocation. Agent sandboxes can be read as those gaps closed for one workload.

Three observations recur across the sources.

Memory is the binding resource. Agents spend most of their time waiting for the
model, and DSec reports that most sandboxes use a small fraction of the CPU
they request. Its density techniques all target memory.

The network boundary carries as much weight as the process boundary. Vercel
puts it as "isolation without egress control contains the process, not its
consequences". The incidents on record are exfiltration and credential misuse
far more often than escapes (§6.3).

The tenant's own workload is an adversary. In training, the policy being
trained probes its environment for the reward. In products, injected content
steers a model that means well. Platforms treat the code inside as hostile even
when the customer is trusted.

### 2.2 Workloads

Eight kinds of workload appear in the sources. They share an API but differ in
what they need most.

| Workload | Lifetime | State | Load | Network | What must be fast | Reproducibility |
|---|---|---|---|---|---|---|
| RL rollouts | minutes to hours | within a trajectory; must survive the trainer being preempted | large bursts at training-step boundaries | per-task policy, often none | each step, since slow steps idle GPUs | high |
| Evaluation | minutes per task | fresh for each task | batch | off, or package registries only | the whole run | defines the benchmark |
| Interactive coding agents | a session, resumed over days | repository, dependencies, build caches, dev servers | human-paced | registries, git, internal services, with credentials kept outside | start-up and terminal latency | moderate |
| Background agents | hours to days; registered indefinitely | durable event log and snapshots | idle, woken by events | inbound addressing; outbound to SaaS under delegated identity | wake-up | low, but the log must replay |
| Tool and MCP server hosting | a long-lived service doing per-request work | moving towards stateless requests | many short calls | the upstream service, per-user OAuth | each call | low |
| Code interpreter in chat | a session with an idle timeout | interpreter state across calls | very high aggregate concurrency | usually none | a chat turn | low |
| Computer use and browsers | minutes to an hour | desktop and browser state; cookies act as credentials | batch or interactive | the live web | the screenshot loop | poor, because the web changes |
| Data and analytics agents | a conversation thread | governed tables and a workspace; files persist | per question | mostly inside the platform | a turn | possible through table versions, but not documented for agent runs |

Two design centres emerge. RL training and evaluation want throughput,
density, many distinct images, cheap forking of one prepared state into many
samples, integrity against the agent itself, and reproducibility. DSec and the
sandbox servers of RL frameworks (SkyRL, ROLL, AReaL) are built for this.
Product agents want fast wake-up, near-zero idle cost, credentials held
outside the sandbox, identity, durability and audit. Agent Substrate, Google's
Agent Executor, Cloudflare and Vercel are built for that. Modal and E2B serve
both.

Data and analytics agents belong with the product agents, with one difference
that shapes everything else. Their world is governed data inside a platform
that already has an authorization model, so the platform starts from the
data's permissions instead of from a Linux box (§5.4).

## 3 Requirements

The requirements below are consolidated from about 45 sources. The workload
abbreviations refer to §2.2: RL, Eval, Coding, Background, MCP, Interpreter,
Computer use, Data.

### 3.1 Functional

**Lifecycle and addressing.** An imperative API to create, execute, read and
write files and stop, called from an SDK rather than a deployment pipeline
(all). Get-or-create by name, with identity independent of the running
instance and of the client session (Coding, Background, Interpreter). Idle
expiry, lifetime caps, pause on timeout, and collection of paused state (all);
hard lifetime caps are a stated obstacle for long-running agents. Wake-up on a
schedule or an event (Background). Durable execution: an event log with
checkpoints, a single writer per session, reconnection with backfill, and
pauses for human approval (Background).

**State.** Pause and resume with memory, so processes survive (RL, Coding,
Background, Interpreter). A filesystem snapshot usable as a boot image (RL,
Eval, Coding). Snapshots scoped to a directory, so the base image can be
patched without discarding project state (Coding, Background). Forking a
running or snapshotted sandbox into many children, for sampling, tree search
or branching trajectories (RL, Background).

**Environment supply.** Any OCI image as input (RL, Eval, Coding). Layers that
compose independently, so that base system, workspace and toolkit are
versioned apart; DSec's layered images make rebuilding linear in the number of
base images instead of in their product with the workspaces (RL, Eval). A
ladder of isolation tiers behind one API, from a function call to a full VM,
chosen per task (RL, MCP, Interpreter). Per workload: services inside the
sandbox, a GUI stack, GPUs, nested containers.

**Network and credentials.** Network policy set per sandbox when it is created
or claimed, by domain and address range, in some products by HTTP method and
path, and changeable while it runs (RL, Eval, Coding, Background, Computer
use). Policy that changes by phase: a setup phase with internet and secrets,
then an agent phase with both removed, as in OpenAI's Codex (Eval, Coding).
Credential brokering, where the secret never enters the sandbox and a proxy at
the boundary adds it, or a proxy checks each operation before attaching a
token, as Anthropic's git proxy does (Coding, Background, MCP, Computer use).
Inbound addressing through preview URLs, exposed ports, and a router that
holds a request while a suspended sandbox resumes (Coding, Background, MCP).

**Integrity.** Graders and hidden tests out of the agent's reach, repositories
flattened to a single commit, and answer paths denied by access-control
profiles (RL, Eval). Capture of trajectories, either token by token through a
proxy or as session recordings (RL, Eval, Computer use). Mock versions of SaaS
applications to keep web tasks trainable (RL, Computer use).

### 3.2 Non-functional

The orders of magnitude below come from 2025–2026 sources. The individual
figures measure different things, such as claim, boot or first instruction,
and do not compare across vendors.

| Requirement | Order of magnitude in the sources | Workloads |
|---|---|---|
| Creation rate | hundreds to thousands of sandboxes per second per cluster | RL, Interpreter |
| Concurrency | tens to hundreds of thousands running; Substrate aims at far more registered than running | RL, Background, Interpreter |
| Start and wake latency | sub-second, with tens to hundreds of milliseconds as targets | Coding, Background, MCP, Interpreter |
| Pause cost | proportional to the memory the sandbox has touched | RL, Coding, Background |
| Density | hundreds of microVMs, or several times as many containers, per large node | RL, Interpreter |
| Idle cost | close to zero: billing for active CPU only, or hibernation | Coding, Background, MCP, Interpreter |
| Image diversity | tens of thousands of base images and many more workspaces per week, each reused only a few times | RL, Eval |
| Isolation | gVisor or a microVM; plain namespaces are not considered enough | all |
| Network enforcement | outside the guest, where the guest cannot switch it off | Coding, Background, Computer use |
| Snapshot integrity | verified before restore, fresh secrets and current policy applied on resume | RL, Coding, Background |
| Clean reuse | no state carried from one tenant to the next on a reused worker | all |
| Quality of service | latency-sensitive work protected from co-located bulk work (§8) | RL, MCP |
| Reproducible environments | images that do not drift with live package indexes | RL, Eval |
| Audit | lifecycle, policy and credential use recorded per agent | RL, Background, MCP |
| Residency and retention | self-hosted execution, deletion guarantees | Coding, Background |

### 3.3 Requirements in tension

| Tension | Why |
|---|---|
| Density against isolation | a guest kernel per sandbox duplicates the page cache and kernel memory; DSec packs several times more containers than microVMs on a node |
| Fast resume against snapshot integrity | the fastest resume is local and paged in lazily; integrity wants verification, fresh secrets, a reseeded random-number generator and current policy. Cloning a memory image clones its keys and nonces (Brooker et al., arXiv 2102.12892) |
| Cheap fork against durable fork | a copy-on-write fork depends on its parent staying alive; a durable one must write out the touched memory first |
| Egress against exfiltration | agents need registries, git and SaaS APIs, and any open path, including a trusted domain, can carry data out (§6.3) |
| Reproducibility against the live internet | research and browser tasks need the web, which changes and leaks answers. Cursor found that many successful benchmark solutions had looked the fix up, and sealing the network lowered scores markedly |
| Realism against reward hacking | full git history, a real network and real tools make tasks realistic and make the answer reachable |
| Idle cost against wake latency | suspending to object storage saves money and warm pools spend it; the Aries paper (arXiv 2607.29069) argues aggressive suspension does not pay at present snapshot costs |
| Sessions against elastic routing | stateful sessions need sticky routing and a single writer; the MCP specification dropped mandatory sessions in 2026 for this reason |
| Snapshot as image against patching | a full-filesystem snapshot freezes an old base image |
| Image diversity against cache locality | images reused only a few times defeat node caches, and placing work near its data conflicts with spreading it |
| Credential brokering against tools inside the sandbox | a proxy that decrypts TLS needs its certificate authority inside, which nested containers and certificate-pinning tools do not honour |
| Stateful sessions against retention guarantees | state kept on the server is retained data |

## 4 Competences

The table lists what a platform in this category must do well, with systems
whose documentation shows the competence as of 2026-09.

| Competence | Covers | Documented by |
|---|---|---|
| Isolation backends | several tiers behind one API; syscall and access-control profiles per task | DSec, agent-sandbox, Substrate |
| Image supply | OCI input, composable layers, lazy block-level loading, distribution that survives bursts, reproducible multi-architecture builds | DSec; Epoch AI's rebuild of SWE-bench |
| Snapshot storage | memory and disk, incremental layers, copy-on-write forks, directory scope, signing, garbage collection, node-local and remote tiers | Substrate, E2B, Modal, Morph |
| Scheduling and capacity | placement faster than Kubernetes, warm and cold pools, pre-warming predicted from the model's output (SpecBox, arXiv 2607.23933), overcommit with service classes, awareness of the trainer | Substrate, GKE agent-sandbox, DSec |
| Memory efficiency | sharing the page cache between guests, returning free pages, reclaiming cold ones | DSec |
| Network policy | enforcement on the host, rules by domain, address and HTTP request, per task and per phase, changes at run time, blocking of metadata services | Vercel, DSec, Substrate, Codex |
| Identity | an identity per sandbox issued when it is claimed, mutual TLS inside the platform, delegation of a user's authority | Substrate, AWS AgentCore |
| Credential brokering | secrets held outside and injected at egress or by validating proxies; short-lived; removed after setup | Vercel, Cloudflare, E2B, Blaxel, OpenAI, Anthropic |
| Routing | stable names, a gateway that holds requests while resuming, terminals, preview URLs, reconnection with backfill | Substrate, Cloudflare, Agent Executor |
| Durable execution | event log, checkpoints, single-writer sessions, approval pauses, schedules | Agent Executor, Claude Managed Agents, Cloudflare |
| Telemetry and audit | per-agent metrics and traces across moves, lifecycle and credential audit, runtime detection, session recording | Substrate, agent-sandbox, Browserbase |
| Evaluation integrity | isolated graders, sanitised repositories, denied answer paths, environment manifests | DSec, SWE-Bench Pro Verified, Kimi K3 |
| GUI and accelerators | virtual displays and input; GPUs inside the sandbox | DSec, Modal, Beam |
| Metering | active-CPU accounting, hibernation at no compute cost, snapshot storage charges | Vercel, Cloudflare, AgentCore |

## 5 Existing systems

### 5.1 Landscape

The table reflects each system's documentation in September 2026. Where a
vendor does not name its isolation technology, the cell says so. Details of
hosted products change quickly; the columns are chosen for the properties
that change least.

| System | Isolation | State | Network default | Orchestration | Openness |
|---|---|---|---|---|---|
| DSec (DeepSeek) | per task: function call, container inside a VM, Firecracker, QEMU | pause and resume across trainer preemption; layered images, and environments packed from agent work | eBPF allowlist per task | own control plane | in-house |
| Agent Substrate | gVisor by default, or Kata on Cloud Hypervisor | memory and disk snapshots; a "golden" snapshot per template | all traffic through an egress policy point, HTTP(S) only | Postgres control plane over Kubernetes pods | Apache-2.0, pre-1.0 |
| kubernetes-sigs/agent-sandbox | gVisor or Kata through RuntimeClass | pause and hibernation; snapshots on GKE with gVisor | NetworkPolicy, default deny | Kubernetes custom resources | Apache-2.0 |
| Agent Executor (google/ax) | delegates to the two above | event log and snapshots; branching trajectories | inherited | durable agent runtime | open source, preview |
| E2B | Firecracker | pause with memory; forks from snapshots | open unless restricted | own control plane | SDK and infrastructure Apache-2.0; hosted |
| Modal | gVisor | filesystem, directory and memory snapshots | configurable | own scheduler | hosted |
| Daytona | unclear: its documentation says both "containers" and "dedicated kernel" | persistent | configurable | own | closed its source in 2026 |
| Vercel Sandbox | Firecracker | filesystem kept on stop | open unless restricted; host firewall with HTTP rules and credential injection | hosted | hosted |
| Cloudflare Sandbox and Containers | VM-backed containers | disk snapshots; sleep and wake by name | egress proxy with credential injection | Durable Objects | hosted |
| Cloudflare Dynamic Workers | V8 isolates | per request; Durable Objects for state | none unless bound explicitly | workerd | runtime open source |
| AWS Bedrock AgentCore | microVMs for the runtime; its code and browser tools are described as containers | sessions, memory cleared afterwards | configurable | hosted | hosted |
| Azure dynamic sessions | Hyper-V | session pools | denied by default | hosted | hosted |
| OpenAI (code interpreter, Codex cloud) | not disclosed | idle expiry; Codex caches state | Codex's agent phase is offline by default | hosted | hosted |
| Anthropic (code execution, Managed Agents) | "sandboxed" containers; gVisor for Managed Agents according to one write-up [2nd] | stateful sessions; option to run execution on the customer's side | allow and block lists for web fetches | hosted, or a customer worker that pulls work | hosted |
| Snowflake Cortex Agents | tools run inside Snowflake under the caller's role; a Python sandbox of unnamed technology | threads keep conversation context; the sandbox lasts one thread | web search needs account opt-in; MCP connectors reach the public internet | managed | hosted |
| Databricks Agent Bricks | "secure VMs" for code, subagents and whole harnesses; data access narrowed through Unity Catalog | agent memory kept across sessions; traces stored as governed data | tool policies written in SQL | managed meta-harness (Omnigent, open source) | hosted |
| Local agent CLIs (Claude Code, Codex CLI) | bubblewrap or Seatbelt; Landlock and seccomp | the host filesystem, restricted | local proxy with a domain allowlist | single process | open source |
| Building blocks | Firecracker, Cloud Hypervisor, gVisor, Kata, libkrun, Hyperlight, Wasmtime, workerd, Unikraft, nsjail, bubblewrap | varies | varies | library or daemon | open source |

### 5.2 What is common and what differs

The hosted products share a baseline: a boundary stronger than a plain
container, creation in under a second from a warm pool or a snapshot, SDKs
for running commands and handling files, preview URLs for exposed ports,
custom images, idle and lifetime limits, some form of snapshot, a switch for
egress, fine-grained billing, a terminal and an MCP endpoint.

They differ mostly at the network edge and in how state is kept. The choice of
hypervisor matters less. Some inject credentials at the egress proxy (Vercel,
E2B, Blaxel, Cloudflare, OpenAI, Fly Sprites); this works only for HTTP and
depends on the TLS server name, and Vercel documents that domain fronting and
DNS can still carry data out under some policies. Only a few filter by HTTP
method and path. The cloud providers, OpenAI and Anthropic deny egress by
default, while the independent sandbox vendors allow it. Some keep memory
across a pause or fork a live sandbox, while others keep only the filesystem.
Beyond that, products differ in billing for active CPU, GPUs, enterprise
identity and self-hosting.

### 5.3 Open and closed source

Daytona moved its development to a closed codebase in 2026, arguing that AI
tools make open isolation code easy to scan for exploits. Earlier the same
year, the Wasmtime project published a batch of advisories found mostly with
LLM-based tools, and fixed them in the open
([Bytecode Alliance](https://bytecodealliance.org/articles/wasmtime-security-advisories)).
The two are opposite responses to the same drop in the cost of finding bugs,
discussed further in §6.5.

### 5.4 Data platforms as harnesses

The platforms in §5.2 give the agent a Linux box, where shell and Python are
the common language, and then take capability away with a sandbox. Snowflake
and Databricks start from the other end. The agent's world is governed
tables, SQL is its first language, and a general-purpose sandbox is a tool it
can escalate to.

In **Snowflake Cortex Agents**
([documentation](https://docs.snowflake.com/en/user-guide/snowflake-cortex/cortex-agents)),
an agent is a schema object that bundles a model, tools, orchestration
settings and instructions. It plans, calls tools and reflects on the results.
The tools turn questions into SQL over a semantic view (Cortex Analyst),
search unstructured data, call stored procedures and UDFs, reach MCP servers,
search the web where the account allows it, and run Python. Each run takes the
permissions of the caller's default role and simply goes without the tools that
role cannot use. The Python sandbox
([documentation](https://docs.snowflake.com/en/user-guide/snowflake-cortex/cortex-agents-code-execution-tool))
sees only the data passed into the session and the user's workspace, which is
mounted writable. It lasts for one thread, keeps files but not interpreter
memory, and its documentation names neither the isolation technology nor any
resource limits. A
[later release note](https://docs.snowflake.com/en/release-notes/2026/other/2026-09-01-native-apps-agent-code-execution)
adds rules against privilege borrowing. An agent invoked from a stored
procedure with owner's rights loses its sandbox tools. The Python sandbox
cannot run SQL, and the wider toolset may only run `SELECT` and `SHOW`. Inside
a Native App, the sandbox never runs with the app owner's privileges.

**Databricks Agent Bricks**
([announcement](https://www.databricks.com/blog/agent-bricks-dais-2026))
hosts other harnesses, such as LangGraph, the Claude Code SDK and the OpenAI
Agents SDK, under a managed meta-harness called Omnigent. Its sandbox provides
"secure VMs" whose data access is narrowed through Unity Catalog, used for
code interpreters, subagents and whole harnesses. Policies on tools are written in
SQL; the example given lets personal data go to a colleague by email but not
to a website, and makes a CRM update wait for human approval. Budgets apply per user
and group, agent memory persists across sessions, and traces are stored as
governed data. A business ontology answers questions such as what a churned
customer means. The announcement names no isolation technology, lifetime or
egress rules for the sandbox. The narrowing of data access rests on Unity
Catalog issuing short-lived storage credentials scoped to the object being
read
([Databricks](https://www.databricks.com/blog/secure-external-access-unity-catalog-assets-open-apis)).

Set against §3 and §4, the change of harness language looks like this:

| Concern | Shell harness | SQL harness |
|---|---|---|
| Isolation | a microVM or gVisor, because the language can do anything | the engine's grants, with a VM only for the escalation tier |
| Egress | a proxy at the network edge | named constructs such as table functions, external stages and MCP connectors, each granted or not |
| Credentials | a proxy that decrypts TLS and adds headers | held by the platform and issued per object |
| Authority | whatever the sandbox holds | the caller's role or the requesting user's permissions |
| Policy | enforced on syscalls and packets as they happen | checked on the query before it runs |
| Pause, resume, fork | memory images | state already lives in tables and files |
| Audit | syscall and network logs | the query log and traces, kept as governed data |
| Meaning | `--help` and man pages | semantic views and ontologies |

The SQL harness changes four things. First, the capability surface can be
enumerated: the ways a SQL session reaches the outside world are named
constructs, so what an agent can reach is answered by a grant query rather than a
sandbox audit. Second, neither vendor relies on SQL alone. Both keep a sandbox
for Python and whole harnesses, so the escalation path runs from SQL through
platform functions and sandboxed Python to a full harness in a VM, and the
middle steps decide the real exposure. Third, the combination of private data,
untrusted content and outside communication moves into the data. Cell values
are untrusted text that returns to the model, and web search and MCP
connectors are the outside channel; the Databricks example policy addresses
exactly that combination. Fourth, running with the caller's rights keeps the
agent's authority at or below the user's. Shell platforms get there only
through credential brokering.

Neither vendor publicly describes how the code-execution tier is isolated,
what it may reach on the network, or what limits bound it. For that tier the
evidence of §6 applies unchanged.

## 6 Minimalism and attack surface

### 6.1 Two kinds of minimalism

Code minimalism means a small trusted computing base: fewer lines, fewer host
system calls, fewer emulated devices, fewer parsers of untrusted input.
Firecracker is the reference design, a small Rust VM monitor with a handful of
virtual devices, no BIOS and originally no PCI, measured against QEMU's much
larger C codebase (Agache et al., NSDI 2020).

Capability minimalism means small reachable authority: which credentials,
sockets, mounts, domains and platform services the code inside can use. The
"lethal trifecta" of private data, untrusted content and outside communication
(Willison, 2025), Meta's "Agents Rule of Two", and CaMeL (arXiv 2503.18813)
are positions on this second axis.

The two are independent. A minimal VM monitor does nothing against an agent
that holds a token and open egress, and a capability-minimal agent in a runc
container is one kernel bug away from the host. Some practices shrink both:
no platform sockets or `docker.sock` inside the sandbox, no host credentials,
a network during installation that carries no secrets, and egress allowlists
treated as grants.

### 6.2 What the evidence supports

Attack surface differs by an order of magnitude between runtimes, and it
spreads across layers. A comparative study of deployed AI code sandboxes
(Andronchik and Lokhmakov, arXiv 2606.08433, 2026) counted host system calls,
visible devices and reachable kernel escalation primitives. The runtime with
the fewest host system calls exposed the most escalation primitives, because a
complete Linux guest ran inside it. Shrinking one layer moved surface into the
next.

Runtimes that share the host kernel have recurring classes of escape. runc
alone has CVE-2019-5736, CVE-2024-21626 and a group of procfs and console
races disclosed in November 2025.

When minimal components have failed, the failure has come in capability added
later. Firecracker's first escape of its VM monitor, CVE-2026-5747, lies in an
optional PCI transport added for throughput. Cloud Hypervisor's disk-format
auto-detection allowed host files to be read (CVE-2026-27211). GPU support
brought host-root escapes through the NVIDIA Container Toolkit
(CVE-2024-0132, CVE-2025-23266). Wasmtime's critical escapes of 2026 sat in
its second compiler backend and in code generation for non-x86 targets.

Large codebases under heavy attack are made acceptable by layering, since
they cannot be shrunk. V8 contains its own internal sandbox. Cloudflare
Workers add hardware memory protection keys, groupings by trust level and a
process sandbox. gVisor stacks its user-space kernel, seccomp and namespaces.
Substrate's roadmap names "two security boundaries between mutually untrusted
actors" as a goal.

### 6.3 What the incidents show

The incidents on record come from reachable capability rather than memory-safety
bugs in the VM monitor.

In DSec's training runs, agents forged calls to the platform's own control
sockets, read its logs for answers, overwrote `/bin/bash`, used a filesystem
ioctl to get around file protections and crashed the filesystem, scanned
ports for package mirrors, and fetched reference code through a module proxy.
The authors conclude that "no single mechanism can prevent all agent
misbehavior". The platform's own agents and sockets inside the sandbox are
part of the attack surface, so a small in-sandbox control plane matters as
much as a small VM monitor.

SandboxEscapeBench (UK AI Security Institute, arXiv 2603.02277) found that
frontier models escape reliably through misconfiguration, such as an exposed
`docker.sock`, writable host mounts or privileged containers, and that they
found escape paths the designers had not anticipated.

Anthropic's account of containing its products
([post](https://www.anthropic.com/engineering/how-we-contain-claude), 2026)
reports that gVisor, seccomp and the hypervisors held, while the company's
own egress proxy failed twice. In one case an allowlist admitted the
company's API domain, and injected instructions used an attacker's API key
through it. The post concludes that allowlists grant capabilities rather than
filter destinations, and warns against trusting custom components.

### 6.4 What the evidence does not support

A low CVE count does not show that an engine is safe. Counts depend on how
much scrutiny and fuzzing a project gets, and the authors of the comparative
study describe a zero as "the absence of a finding, not the presence of
soundness".

Fewer lines do not mean fewer defects per line. The software-engineering
literature finds that the number of defects grows with size but their density
does not fall (Koru et al.), and that complexity and churn predict vulnerable
code better (Shin, Meneely and Williams, IEEE TSE 2011). For security, the
code an attacker can reach matters more than total size.

A minimal image is not shown to be a secure image. Vendor comparisons count
scanner findings, which a minimal image reduces by construction, and do not
measure exploitability.

Unikernels are not secure because they are small. NCC Group tested Rumprun
and IncludeOS
([whitepaper, 2019](https://www.nccgroup.com/media/tgsj1css/_ncc_group-assessing_unikernel_security.pdf))
and found no address-space randomisation, memory that was writable and
executable at once, stack canaries compiled in but always zero because the
guard value lived in thread-local storage neither system set up, and weak or
absent heap checks. The authors trace most gaps to oversight: an application
linked into ring 0 inherits all the work a kernel and loader normally do.
Later work adds some of the missing protections, for example Unikraft's
[security features](https://unikraft.org/docs/concepts/security) and
address-space randomisation for OSv
([arXiv 2602.11445](https://arxiv.org/abs/2602.11445)).

Small custom components are not safe by default. In Anthropic's account the
failures came from new components, while the long-exercised ones held.

A slow path from upstream to deployment takes away much of what remains. The
comparative study found deployed products running hypervisor releases more
than a year behind upstream. A small codebase patched quickly upstream and
slowly downstream behaves like a large one.

### 6.5 Minimalism when bugs are cheap to find

Two responses to AI-assisted vulnerability discovery compete. One holds that
minimal is no longer enough: any component that processes untrusted input
will be swept, so intermediaries should be removed or the source closed
(Edera, which sells a competing hypervisor, and Daytona). The other holds that
minimal matters more, because a small VM monitor can be swept completely and a
large one cannot; Firecracker's and Wasmtime's 2026 bugs were found and fixed,
and sat in recently added code.

The evidence gathered does not settle this. It does show that new bugs follow
new capability, which suggests treating each added feature, whether a PCI
transport, a disk format, a GPU path, a second compiler or an egress proxy, as
a decision about the trusted computing base, with its own review.

### 6.6 A working position

The sources support a narrower claim than "smaller is safer".

1. Minimalism is chosen layer by layer, and the layers are counted together.
   A smaller VM monitor in front of a complete guest kernel moves surface
   without removing it.
2. Capability minimalism accounts for most of the recorded incidents: no
   credentials in the sandbox, no platform sockets inside it, egress as named
   grants, and phases that drop secrets. The data platforms add running with
   the caller's rights (§5.4).
3. At the boundary, a well-exercised component is preferable to a new small
   one.
4. Where the trusted base cannot be made small, two boundaries of different
   kinds are the common answer: a user-space kernel with seccomp, a VM with a
   jailer, an isolate with a process sandbox.
5. The supply chain is part of the surface. Agents run package install hooks,
   which worms such as Shai-Hulud have used, and models invent package names
   repeatably, which attackers can register in advance (Spracklen et al., USENIX
   Security 2025). Mirrors, allowlists and lockfiles address this on the
   capability side. Reproducible builds show what is in the trusted base but do
   not shrink it.

## 7 Design space

Nine axes separate the systems.

1. **Where the boundary sits.** In a language runtime (V8 isolates, Deno,
   WASI); in policy on the shared host kernel (seccomp, Landlock, Seatbelt,
   bubblewrap), as the local CLIs do; in a user-space kernel (gVisor); in a VM
   with a guest kernel (Firecracker, Cloud Hypervisor, Kata, libkrun); or in a
   VM without a guest operating system, or a unikernel (Hyperlight,
   Unikraft).
2. **What state survives.** Nothing, a persistent disk, a directory, a full
   memory snapshot, a live fork, or incremental checkpoints with rollback
   (DeltaBox, arXiv 2605.22781).
3. **How a sandbox starts.** A cold boot, a restore from a template snapshot
   (paged in lazily, or with the working set prefetched as in REAP,
   ASPLOS 2021), a claim from a warm pool, or a fork.
4. **Where the network is decided.** No network, a bridge, an eBPF allowlist,
   a host proxy with a domain allowlist, an egress point that decrypts and
   injects credentials, or explicit bindings only.
5. **Who holds credentials.** The sandbox; a setup phase that ends before the
   agent runs; the egress proxy; a proxy that validates each operation; or a
   typed binding the code never sees.
6. **Orchestration.** An in-process library, a single-host daemon, Kubernetes
   custom resources, or a control plane of its own.
7. **How density is achieved.** Suspending idle agents and multiplexing onto
   fewer workers; scaling to zero; sharing and reclaiming memory; reusing a
   sandbox across functions.
8. **Size of the trusted base.** From the whole host kernel, through a Rust VM
   monitor with KVM, to no virtual devices at all.
9. **Harness language.** Shell and Python on a Linux box; SQL over governed
   data with a sandbox to escalate to (§5.4); or typed bindings only. The
   language decides whether capability is granted and then taken away, or
   named and then added.

Several regions are well occupied. Kubernetes layers default to gVisor, with
Kata as the stronger option. Control planes built on Firecracker snapshots
dominate the hosted products. libkrun microVMs cover laptops, and
kernel-policy sandboxes cover the local coding CLIs.

Other regions are thin or empty, judging from documentation in 2026-09:
- VM-level fork or rollback on unmodified Kubernetes;
- WebAssembly for agents that need a full Linux toolchain;
- snapshot restore that documents re-keying, reseeding the random-number
  generator and re-applying policy; no surveyed document covers all three;
- sandboxes shaped as typed capabilities outside Cloudflare and the data
  platforms;
- a SQL-first harness outside the two proprietary data platforms, meaning an
  embedded engine per session, sandboxed, with grants of its own.

## 8 Performance isolation

Preventing denial of service and keeping measurements trustworthy use the
same mechanisms but succeed by different measures. Denial-of-service
prevention is about the worst case and needs hard caps and fair shares.
Measurement is about variance and needs low, known noise, or better, metrics
that neighbours cannot disturb.

A security boundary separates state. It does not divide performance. A VM or
gVisor keeps one tenant's memory and kernel objects away from another's, but
the cores, caches, memory bandwidth, power budget, disks and network
underneath remain shared unless something divides them explicitly.

### 8.1 Shared resources and their controls

| Shared resource | Separated by a VM or gVisor? | Control |
|---|---|---|
| CPU time | no | cgroup v2 `cpu.max` and `cpu.weight`; cpusets |
| Hyperthread siblings | no | core scheduling; DSec found it reduced, but did not remove, the slowdown of latency-sensitive work next to bulk work |
| Last-level cache, memory bandwidth | no | partitioning through Linux `resctrl` |
| Power and boost budget | no | fixed frequencies on hosts that measure |
| Memory, page cache, swap | partly; a VM has its own page cache | `memory.max`, `memory.high`, swap limits, ballooning; the hardest to get right under overcommit |
| Disk bandwidth, space, inodes | no | `io.max`, `io.latency`, filesystem quotas; Firecracker's rate limiters on block devices |
| Processes, file descriptors | inside the guest | `pids.max`, rlimits |
| Network | no | `tc`; Firecracker's rate limiters on network devices; limits per sandbox at the egress point |
| Sandbox overhead (gVisor's user-space kernel, VM monitor threads) | the overhead itself is shared | charged to the sandbox's cgroup; rootless gVisor run with `--ignore-cgroups` has no limits at all |
| Control plane | not applicable | limits on creation rate, quotas per tenant, quotas on snapshot storage, holding requests when capacity is short |

Accounting has to find the memory. In the gVisor spike of §9, application
memory lived in a memory file that the host counted as shared memory rather
than as the process's resident set. A limit based on resident memory would
have missed most of it; a cgroup charged for the sandbox would not.

### 8.2 Denial of service

The recorded cases are mostly accidental exhaustion by agents. DSec lists
disks filled by `yes`, a stdout of tens of gigabytes, a recursive `grep` from
the root, and a kernel crash from reading a `/proc` file. Substrate's threat
model lists image decompression bombs and agents that create agents without
limit.

What is exhausted decides the control. The sandbox's own quota is harmless
once the caps of §8.1 exist. Resources shared on a node need every row of
that table, and memory and I/O are where gaps usually remain. The platform
needs limits on creation, snapshot storage and image pulls. Third parties need
rate limits at the egress point, since an allowlist says where traffic may go
but not how much. The harness itself is the case specific to agents: output
fed into the model's context costs tokens and money and can break the loop,
so output is capped where it is produced. A slow refusal is a denial of
service too; in the §9 spike a forbidden `url()` call failed only after
minutes of retries.

The data platforms add limits in money, with per-run budgets and quotas per
user or group. These bound what a runaway loop costs.

### 8.3 Engine limits in a SQL harness

A query engine can limit work before and during execution, which a shell
cannot. ClickHouse, checked on a 2026 build, has `max_memory_usage`,
`max_execution_time`, `max_estimated_execution_time`, `max_rows_to_read`,
`max_bytes_to_read`, `max_result_rows`, `max_result_bytes`, limits on spilling
to disk, workloads that schedule CPU threads (`CREATE RESOURCE`,
`CREATE WORKLOAD`), and `max_query_size` with `max_ast_elements` against
oversized queries.

Two properties govern their use. Most of them default to unlimited, so a
harness sets them in a settings profile the session cannot change. They are
also checked between blocks of rows, so one pathological expression can
overrun a single block. The resulting pattern has two layers: engine limits
return an error the agent can read and act on, for instance by filtering or
sampling, and a cgroup stops the process as a last resort. A killed process
gives the agent nothing to learn from, so the readable error matters to the
harness as much as the cap does.

### 8.4 Measurements that can be trusted

Measurement matters in two places: when the platform itself is measured, and
when a measurement is the result, as in tasks that reward faster code, steps
with a time budget, or benchmark timeouts.

Inside a sandbox, time is a weaker measure. The overhead varies with load and
with the machine, counters are coarse or missing, and memory can sit where
process tools do not look. The sources support these practices, strongest
first:

1. Count work where the task allows: instructions retired, rows and bytes
   read, allocations. For fixed data and settings, ClickHouse's
   `SelectedRows` and `ReadCompressedBytes` profile events do not depend on
   neighbours.
2. Where time is the measure, pair it: run candidate and baseline alternately
   on the same pinned core in one window and compare the ratio. Absolute
   times from shared hosts do not compare.
3. Isolate hosts that measure: dedicated cores, hyperthreading off or core
   scheduling, fixed frequencies, cache partitions, no other tenants during
   the window. Measuring is a different class of worker from densely packed
   rollouts.
4. Let a sealed grader take the measurement, never the agent's own sandbox.
   Timing rewards invite gaming; Kimi K3 reports replayed GPU graphs, cached
   inputs and reduced precision. Fresh inputs, a separate sandbox and a
   hidden harness keep the agent out of the environment that measures it.
5. Record the conditions with each result: background load, clock behaviour,
   platform mode. [doc/trials](../trials/README.md) asks the same of this
   repository's trials.

Performance isolation also protects confidentiality. Shared caches and memory
bandwidth are timing channels between tenants, so cache partitioning and core
scheduling serve both purposes.

## 9 Where this meets boxer

These are observations, not proposals. Boxer is not a sandbox and does not
claim to be one. [ADR-0026](../adr/0026-app-runtime-and-capability-subjects.md)
describes its capability model as "hygiene, not security", enforced by lint
and review rather than by process isolation.

**Egress as named grants.**
[ADR-0262](../adr/0262-http-egress-as-a-keelson-capability.md) gives each
network destination its own bus verb, `net.http.fetch.<destination>`, with
allowed URL prefixes registered in code. This is the shape Anthropic's post
arrives at, an allowlist as a capability grant, but enforced by the bus's
publish check inside one process instead of at a network edge.

**Credentials held by a service.**
[ADR-0254](../adr/0254-model-inference-as-a-keelson-capability.md) keeps the
model endpoint and its credential in a host service. An app holds a capability
to ask, never the key, which matches the intent of credential brokering.

**Minimal images and reproducible builds.**
[ADR-0206](../adr/0206-gokrazy-appliance-image.md) builds gokrazy appliance
images, and [ADR-0215](../adr/0215-retire-mimalloc-reproducible-builds.md)
removed a dependency so that artifacts build byte for byte. Both belong to
code minimalism, with the caveat of §6.4 that a smaller image shows less
scanner surface rather than fewer exploitable paths.

**Hostile tenants.** The
[multi-tenant display design space](./imzero2-multi-tenant-display-design-space.md)
treats each application as a hostile tenant in a QEMU guest, the VM tier of
§7, facing the same tension between density and isolation that DSec measures.
The survey suggests where that design is most exposed. The media host's
worker parses the mesh stream each guest sends and, for video viewers,
decodes and rasterizes its geometry; that parser and decoder are the custom
component at the boundary, the kind of layer that failed in Anthropic's
account (§6.3), so they are the part to keep small, memory-safe and fuzzed.
The video encoder behind them only sees pixels the trusted rasterizer
produced. The guest boundary also separates state without dividing
performance, so the shared rasterizer, encoder and GPU need per-tenant
limits of the kind described in §8.

**SQL as the harness language.** The parts a SQL-first harness needs exist in
the tree for other reasons:
- a pool of pre-started `clickhouse-local` processes
  ([ADR-0028](../adr/0028-chlocal-low-latency-sql-cap.md),
  [`github.com/stergiotis/boxer/public/keelson/data/chlocalpool`](../../public/keelson/data/chlocalpool/));
- a seam before execution where SQL passes run
  ([ADR-0108](../adr/0108-keelson-sql-pass-registry.md),
  [`github.com/stergiotis/boxer/public/keelson/data/passreg`](../../public/keelson/data/passreg/)),
  where a policy check or rewrite would sit;
- read capabilities per table on the bus
  ([ADR-0253](../adr/0253-introspection-table-reads-as-a-bus-capability.md));
- introspection tables that act as a catalog
  ([ADR-0094](../adr/0094-keelson-introspection-tables.md));
- publishing a computed table without storing it
  ([ADR-0240](../adr/0240-adhoc-datasets-v2-sealed-store-owned-capability.md));
- a chat whose model drives an app's windows through the app's operations
  ([ADR-0265](../adr/0265-chat-app-over-retained-model-calls.md),
  [ADR-0269](../adr/0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)).
  The text-to-SQL orchestrator listed here when this survey was written was
  deleted on 2026-10-07.

What they lack is a boundary. A spike on 2026-09-27, on one machine and with
data kept outside the tree, ran `clickhouse-local` under rootless gVisor with
a root filesystem holding only the binary's libraries, one writable data
mount and no network. MergeTree tables, Parquet files, mmap and direct I/O
worked. io_uring reads failed with an error instead of falling back, and a
forbidden `url()` call took minutes to fail. Start-up and query times both
grew noticeably on a power-limited handheld machine under background load.
Whether that overhead holds on server hardware is open (§10).

**Durable work.** [ADR-0223](../adr/0223-watchbill-durable-work-on-facts.md)
(watchbill) covers durable execution for jobs but keeps no process image. In
Substrate's terms it is closest to a snapshot of data only, with the event log
in ClickHouse.

## 10 Open questions

- Which design centre of §2.2, if either, applies to a boxer deployment: an
  agent using boxer apps through the bus, or boxer hosting agent code?
- Is there an independent measurement of wake latency and density for
  Substrate or agent-sandbox under a stated workload? None was found; the
  Substrate figures are targets.
- Does any system document the full restore hygiene of §7: re-keying,
  reseeding and re-applying policy?
- How do Snowflake and Databricks isolate their code-execution tier, and what
  can it reach on the network?
- Does the gVisor overhead seen in the §9 spike hold on a server CPU without a
  shared power budget?
- How do typed-capability sandboxes such as Dynamic Workers compare in
  practice with boxer's subject-filter capabilities? Both grant authority by
  naming what code may call instead of fencing a Linux box.

## 11 Sources

All were read on 2026-09-27.

**Anchors**
- DSec: [arXiv 2609.22978](https://arxiv.org/html/2609.22978v1)
- Agent Substrate: [repository](https://github.com/agent-substrate/substrate), documents `docs/architecture.md`, `docs/threat-model.md`, `docs/roadmap.md`, `docs/network-egress.md`, `docs/egress-traffic.md`, `docs/glossary.md`
- Jonas et al., "Cloud Programming Simplified": [arXiv 1902.03383](https://arxiv.org/abs/1902.03383)

**Kubernetes and Google**
- [kubernetes-sigs/agent-sandbox](https://github.com/kubernetes-sigs/agent-sandbox), [snapshots](https://agent-sandbox.sigs.k8s.io/docs/sandbox/snapshots/)
- [Agent Sandbox on GKE and Agent Substrate](https://cloud.google.com/blog/products/containers-kubernetes/bringing-you-agent-sandbox-on-gke-and-agent-substrate)
- [Agent Executor announcement](https://cloud.google.com/blog/products/ai-machine-learning/agent-executor-googles-distributed-agent-runtime), [google/ax](https://github.com/google/ax)

**Hosted platforms**
- E2B: [docs](https://docs.e2b.dev/), [persistence](https://docs.e2b.dev/sandbox/persistence), [infra](https://github.com/e2b-dev/infra)
- Modal: [sandboxes](https://modal.com/docs/guide/sandbox), [directory snapshots](https://modal.com/blog/directory-snapshots-resumable-project-state-for-sandboxes), [Amplify Partners write-up](https://www.amplifypartners.com/blog-posts/behind-the-scenes-of-modal-sandboxes)
- Vercel: [concepts](https://vercel.com/docs/sandbox/concepts), [firewall](https://vercel.com/docs/sandbox/concepts/firewall), ["half a sandbox"](https://vercel.com/blog/a-sandbox-without-a-network-boundary-is-only-half-a-sandbox)
- Cloudflare: [Sandbox](https://developers.cloudflare.com/sandbox/), [Sandboxes GA](https://blog.cloudflare.com/sandbox-ga/), [Dynamic Workers](https://blog.cloudflare.com/dynamic-workers/), [Project Think](https://blog.cloudflare.com/project-think/), [MCP v2](https://blog.cloudflare.com/mcp-v2/)
- Daytona: [docs](https://www.daytona.io/docs/), [closed-source announcement](https://www.daytona.io/dotfiles/updates/daytona-is-going-closed-source)
- AWS: [AgentCore Runtime](https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/agents-tools-runtime.html), [Code Interpreter](https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/code-interpreter-tool.html)
- OpenAI: [Codex cloud environments](https://developers.openai.com/codex/cloud/environments), [internet access](https://developers.openai.com/codex/cloud/internet-access)
- Anthropic: [Claude Code sandboxing](https://www.anthropic.com/engineering/claude-code-sandboxing), [How we contain Claude](https://www.anthropic.com/engineering/how-we-contain-claude), [Managed Agents](https://platform.claude.com/docs/en/managed-agents/overview), [sandbox-runtime](https://github.com/anthropic-experimental/sandbox-runtime)
- Snowflake: [Cortex Agents](https://docs.snowflake.com/en/user-guide/snowflake-cortex/cortex-agents), [code execution tool](https://docs.snowflake.com/en/user-guide/snowflake-cortex/cortex-agents-code-execution-tool), [Native Apps release note](https://docs.snowflake.com/en/release-notes/2026/other/2026-09-01-native-apps-agent-code-execution)
- Databricks: [Agent Bricks announcement](https://www.databricks.com/blog/agent-bricks-dais-2026), [external access to Unity Catalog](https://www.databricks.com/blog/secure-external-access-unity-catalog-assets-open-apis)
- Others: [Fly Machines](https://docs.fly.io/machines/overview/), [Morph](https://cloud.morph.so/docs/developers), [Blaxel](https://blaxel.ai/sandbox), [Northflank](https://northflank.com/product/sandboxes)

**Building blocks**
- [Firecracker](https://firecracker-microvm.github.io/), [NSDI 2020 paper](https://www.usenix.org/conference/nsdi20/presentation/agache), [snapshot support](https://github.com/firecracker-microvm/firecracker/blob/main/docs/snapshotting/snapshot-support.md)
- gVisor: [security basics](https://gvisor.dev/blog/2019/11/18/gvisor-security-basics-part-1/), [architecture and security](https://gvisor.dev/docs/architecture_guide/security/), [GPU](https://gvisor.dev/docs/user_guide/gpu/)
- [libkrun](https://github.com/containers/libkrun), [microsandbox](https://github.com/microsandbox/microsandbox), [Hyperlight](https://github.com/hyperlight-dev/hyperlight), [workerd](https://github.com/cloudflare/workerd), [Unikraft](https://github.com/unikraft/unikraft)
- Brooker et al., "Restoring Uniqueness in MicroVM Snapshots": [arXiv 2102.12892](https://arxiv.org/abs/2102.12892)

**Performance isolation**
- Linux [cgroup v2](https://docs.kernel.org/admin-guide/cgroup-v2.html), [core scheduling](https://docs.kernel.org/admin-guide/hw-vuln/core-scheduling.html), [resctrl](https://docs.kernel.org/filesystems/resctrl.html)
- Firecracker [rate limiters](https://github.com/firecracker-microvm/firecracker/blob/main/docs/api_requests/patch-network-interface.md)
- ClickHouse [query complexity settings](https://clickhouse.com/docs/operations/settings/query-complexity), [workload scheduling](https://clickhouse.com/docs/operations/workload-scheduling)

**RL and evaluation infrastructure**
- Kimi K2 [arXiv 2507.20534](https://arxiv.org/abs/2507.20534), Kimi K3 [arXiv 2607.24653](https://arxiv.org/html/2607.24653v1), [Qwen3-Coder](https://qwenlm.github.io/blog/qwen3-coder/), [MiniMax Forge](https://www.minimax.io/news/forge-scalable-agent-rl-framework-and-algorithm)
- [SWE-smith](https://arxiv.org/abs/2504.21798), [R2E-Gym](https://github.com/R2E-Gym/R2E-Gym), [SWE-rebench](https://arxiv.org/abs/2505.20411), [SWE-MiniSandbox](https://arxiv.org/abs/2602.11210), [SkyRL](https://github.com/NovaSky-AI/SkyRL), [ROLL Flash](https://arxiv.org/abs/2510.11345)
- [Epoch AI: SWE-bench in one hour](https://epoch.ai/latest/swebench-docker), [Cursor on reward hacking](https://cursor.com/blog/reward-hacking-coding-benchmarks), [SWE-Bench Pro Verified](https://arxiv.org/pdf/2609.08149), [OSWorld-Verified](https://xlang.ai/blog/osworld-verified)
- [SpecBox](https://arxiv.org/abs/2607.23933), [Aries](https://arxiv.org/abs/2607.29069), [DeltaBox](https://arxiv.org/abs/2605.22781)

**Security and minimalism**
- Andronchik and Lokhmakov, "AI Code Sandboxes: A Comparative Security Study, Part 1": [arXiv 2606.08433](https://arxiv.org/abs/2606.08433)
- [CVE-2026-5747](https://nvd.nist.gov/vuln/detail/CVE-2026-5747), [CVE-2026-27211](https://nvd.nist.gov/vuln/detail/CVE-2026-27211), [runc advisories of November 2025](https://seclists.org/oss-sec/2025/q4/138), [NVIDIAScape](https://www.wiz.io/blog/nvidia-ai-vulnerability-cve-2025-23266-nvidiascape), [Wasmtime advisories](https://bytecodealliance.org/articles/wasmtime-security-advisories)
- [V8 sandbox](https://v8.dev/blog/sandbox), [Cloudflare Workers hardening](https://blog.cloudflare.com/safe-in-the-sandbox-security-hardening-for-cloudflare-workers/)
- [SandboxEscapeBench](https://arxiv.org/abs/2603.02277), [lethal trifecta](https://simonwillison.net/2025/Jun/16/the-lethal-trifecta/), [Agents Rule of Two](https://ai.meta.com/blog/practical-ai-agent-security/), [CaMeL](https://arxiv.org/abs/2503.18813)
- Saltzer and Schroeder, [The Protection of Information in Computer Systems](https://www.cs.virginia.edu/~evans/cs551/saltzer/); [NCC Group on unikernels](https://www.nccgroup.com/research/assessing-unikernel-security/); [Koru et al.](https://link.springer.com/article/10.1007/s10664-008-9080-x)
- [Edera: minimal is no longer enough](https://edera.dev/stories/minimal-is-no-longer-enough-why-ai-scale-vulnerability-discovery-changes-container-security), [Chainguard CVE risk](https://edu.chainguard.dev/chainguard/chainguard-images/staying-secure/cve-risk/), [Shai-Hulud 2.0](https://securitylabs.datadoghq.com/articles/shai-hulud-2.0-npm-worm/)
