---
type: adr
status: proposed
date: 2026-07-26
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0145: Sealed app data — one vocabulary, and a wall that is checked

## Context

[ADR-0134](./0134-adhoc-datasets.md) gave a running app a way to hand
tabular data to a SQL applet without creating durable state: the data is
chunk-encrypted on disk, its key lives only in memory, and a query names it
through an unguessable handle. The at-rest half of that guarantee is real
and unconditional — after a crash the files are ciphertext whose key no
longer exists.

The *placement* half is not enforced anywhere. Three unrelated things carry
it today:

- the introspection HTTP server refuses a non-loopback bind at Start
  ([ADR-0082](./0082-imzero2-remote-session-auth-tls.md) §SD1 gate),
  which bounds where the decrypted stream can be read from;
- the broker is in-process, so the key never leaves it;
- an engine adapter refuses a request carrying an extension it does not
  recognise ([ADR-0144](./0144-query-engine-adapters.md)) — which happens
  to include encrypted inputs, but not because it understood them.

And the reason a `keelson('adhoc_…')` buffer pinned at an external
ClickHouse does not reach that server's data path is that a real ClickHouse
does not know what `keelson()` is. That is an accident, and ADR-0134's own
revision names the direction that retires it: "a stepping stone to a native
`keelson()` table function — which would also resolve server-side".

Two findings from reading the shipped wiring rather than the ADRs.

**The concept is spelled four times.** `EncryptedInputRef` (chlocalbroker),
`EncryptedEntry` / `EncryptedDatasetI` (introspect), `DatasetDecryptor`
(introspecthttp), and an `Extra.EncryptedInputs` map reached through an
`any`-typed escape hatch (the ADR-0144 chlocal adapter). Key custody is a
fifth spelling in a fifth place (`KeyStore` / `KeyStoreI`). Nothing is
wrong with any of them individually; together they mean a reader has to
discover that these are one thing.

**One of the two decrypt paths is unreachable.** ADR-0134's 2026-07-20
revision recorded two — the broker's named-pipe route as originally
specified, and the `/table` handler that decrypts in-process and streams
plaintext over loopback — and said the pipe machinery would be "retirable
later" if the url() route proved dominant. In this repository it is not
merely dominant:

- the production `/query` runner (`introspecthost`) submits
  `{SQL, Params}` and nothing else, because `/query` has already rewritten
  `keelson('h')` into `url('…/table/h')`;
- the only code that ever populates `EncryptedInputs` is
  `introspectengine`, and nothing outside tests imports `introspectengine`.

So the pipe route, the `mkfifo` writer, the `EncryptedInputRef` type and
the `any` escape hatch that carries it all exist to serve a path no shipped
configuration takes.

The requirements catalog
([query-system-requirements](../explanation/query-system-requirements.md))
already names what is missing: **R2** hard locality walls a router must be
unable to override, **R3** locality proven by demonstration rather than
inferred from an address, **R6** sensitivity as an axis independent of
execution mode, and the two extension points still marked *delta* — **E6**
the reachability probe, which exists and which nothing consumes, and **E9**
the label vocabulary.

## Design space (QOC)

**Question.** How should app-level encryption be expressed where queries
are placed and executed?

**Options.**

- **O1 — An input type.** Sealed datasets become a named field on the
  engine request; an engine that cannot bind them refuses. The encrypted
  bytes travel with the run.
- **O2 — A sensitivity label.** The run carries what it *touches*;
  placement and delivery both judge it. The data keeps travelling by the
  existing handle-over-loopback route.
- **O3 — Deployment fact plus better diagnostics.** Keep the bind gate as
  the only enforcement; make the failure legible instead of
  "Unknown function keelson".
- **O4 — End-to-end to the engine.** Hand the engine the key and let it
  read ciphertext directly.

**Criteria.**

- **C1 — Survives `keelson()` becoming a native table function**, the
  recorded direction.
- **C2 — Number of decrypt implementations** that must stay correct.
- **C3 — Is the wall checkable**, or is it a property of how the box was
  deployed?
- **C4 — Cost now**, against what actually ships.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −  | ++ | −− | +  |
| C2 | −− | ++ | +  | −  |
| C3 | +  | ++ | −− | +  |
| C4 | −  | −  | ++ | −− |

O4 is killed by a grounding fact rather than by preference: ClickHouse
offers no read path for externally encrypted files (ADR-0134's context
records the `encrypted` disk type and the Parquet modular-encryption gap),
so decryption must happen on our side of the `file()`/`url()` boundary —
and the option would put key material on a wire that today carries only a
handle.

O1 is what "promote the escape hatch to a named field" amounts to. It
dresses up the path nothing takes, and keeps two decrypt implementations
alive to do it.

## Decision

We will treat sealed app data as a **placement property, not an input
format**: one vocabulary for the data, a sensitivity label for the run, two
places that refuse, and the removal of the unreachable path.

- **SD1 — One vocabulary, in `adhocdata`.** That package already owns the
  AEAD stream, the publish/grant/retract service, the structure mapping and
  the wire, so the concept goes where the concept already lives:

  - `Ref{Handle, Structure, Revision}` — what names a sealed dataset.
  - `CustodyI{Register, Lookup, Forget}` — key custody, which
    `chlocalbroker.KeyStore` implements rather than defines.
  - `DecryptorI` — the seam `/table` takes to stream a dataset's
    plaintext.

  `introspect.EncryptedEntry` reports a `Ref`; `introspecthttp` takes a
  `DecryptorI`; `chlocalbroker` supplies custody. Four spellings become
  one, and no package below `adhocdata` has to know what encryption is.

- **SD2 — Retire the pipe route.** `ExecRequest.EncryptedInputs`,
  `EncryptedInputRef`, `materializeEncryptedInputs` and its `mkfifo`
  writer, the chlocal adapter's `Extra` type, and — with its last user
  gone — `queryengine.Request.Extra` are removed. ADR-0134 pre-authorised
  exactly this. What remains is one decrypt implementation: resolve the key
  by handle, stream the AEAD reader out of `/table`.

  The cost is stated rather than hidden: `introspectengine` loses
  encrypted-dataset support. It is a public library seam with no consumer
  in this repository, so the loss is potential rather than actual, and a
  direct in-process consumer that wants it is asking for the second decrypt
  path back.

- **SD3 — The sensitivity label (E9's first axis).** A statement that
  names a sealed handle is **confined**; everything else is ordinary. The
  label is *derived, never guessed*: the macro references a statement
  carries (`keelsonsql.References`) are looked up in the introspection
  registry, and an entry that reports a `Ref` makes the run confined. That
  is a registry lookup, not a heuristic over SQL text, which is what lets
  it be relied on.

  The label type lives in `queryengine` beside the rest of the dispatch
  contract; the *derivation* lives with the resolver, which is the only
  layer that knows a registry. `queryengine` keeps importing nothing but
  the frame contract and the id contract.

  The zero value is `SensitivityOrdinary`, not unknown-and-therefore-denied.
  This is a deliberate departure from the R5 default-deny discipline the
  statement-kind classifier uses, and the reason is that the two answer
  different questions: "is this a mutation?" is undecidable from unparseable
  SQL and must fail closed, whereas "does this run touch sealed data?" is
  decided by a binding this process performed. A run that names no sealed
  handle is ordinary because nothing sealed was bound into it, not because
  we failed to notice.

- **SD4 — Two refusals, deliberately.** The resolver declines to place a
  confined run on an engine that may not see plaintext (R2: a router must
  be *unable* to override a locality wall), and the engine refuses the same
  request regardless (the backstop a site-supplied resolver cannot bypass).

  The engine-side check is a discipline gate rather than a security
  boundary, and the ADR says so: an engine is told at construction whether
  it may serve confined runs. Its value is that forgetting is loud and
  local — a new issuer that never thought about sealed data gets a refusal
  naming the reason, instead of a query that works until the day the
  endpoint moves.

- **SD5 — Locality proven, not inferred (R3), which gives E6 its first
  consumer.** Exactly one engine may serve a confined run without proof:
  this process's own introspection plane, and it is exempt by *identity*
  rather than by address — the endpoint string was minted by a server this
  process started, which is not the same act as recognising `127.0.0.1` in
  a configured URL.

  Any other engine must first have demonstrated that it can fetch from this
  process's loopback plane, using the probe primitive that has been sitting
  unconsumed: mint a single-use nonce URL, have the engine evaluate a
  statement that fetches it, then ask whether it was fetched. A proof is
  bounded in time and per engine endpoint; the caching window is policy and
  belongs to the issuer.

  **What the probe does and does not establish.** It fails safe against
  *accident*: an endpoint pinned at a server elsewhere cannot fetch our
  loopback, so the proof simply fails and the run is refused. It does not
  defend against a *deliberately constructed* reverse tunnel, which would
  let a remote engine fetch our loopback and pass. That is consistent with
  ADR-0134 §SD2's stance rather than a gap in it — the threat model of this
  cryptography is the disk, and an adversary with local privilege is
  already out of scope — but the probe must not be read as a security
  boundary. It is a misconfiguration wall.

- **SD6 — What stays unclaimed.** The threat model does not change.
  Plaintext still transits a loopback socket, which a privileged local
  observer can sniff; intra-process isolation is still not attempted, since
  any code in the runtime can reach the keys; and the catalog still shows
  handles to the operator surface. This ADR narrows *where a query may send
  sealed data*, and nothing else.

## Alternatives

- **Promote the escape hatch to a named `Sealed` field (O1).** Makes the
  request type honest about a path no configuration takes, and commits the
  repository to keeping two decrypt implementations correct forever.
- **Diagnostics only (O3).** Cheapest, and defensible while `keelson()`
  stays a client-side macro — but it is exactly the enforcement that
  evaporates when the macro becomes a server-side table function, which is
  the recorded direction.
- **Give the engine the key (O4).** No ClickHouse read path exists for
  externally encrypted files, and it would put key material on a wire that
  currently carries only a handle.
- **Default-deny the sensitivity label.** Symmetric with the statement-kind
  classifier and wrong here: an unparseable statement names no sealed
  binding, and treating "I could not tell" as "confined" would refuse
  ordinary queries for a property they do not have.
- **Enforce only at the engine.** Simpler, and it loses R2: a site resolver
  would be free to place a confined run anywhere, with the refusal arriving
  after the placement decision rather than instead of it.
- **A separate `sealed` package below everything.** Cleaner layering than
  SD1 — no app-facing package beneath the broker — and rejected as one
  package too many for the number of types involved. Recorded because the
  judgement is reversible and the negative it buys is real.

## Consequences

### Positive

- One decrypt implementation instead of two, in the most security-sensitive
  code in the feature.
- The confinement guarantee stops depending on an accident of macro
  resolution, and survives `keelson()` becoming a native table function.
- `queryengine.Request` loses its `any`-typed field, which was the weakest
  thing in that contract.
- E6 and E9 — the catalog's last two *delta* points — each get a real
  consumer, and R2/R3/R6 stop being requirements nothing implements.
- A refusal names its reason, so "this endpoint may not serve confined
  data" replaces "Unknown function keelson".

### Negative

- A confined run against a non-local engine costs a probe round trip the
  first time, plus a proof-caching policy that did not exist before.
- `introspectengine` loses encrypted-dataset support (SD2), and a
  downstream consumer of that seam — none in this repository — would have
  to move to the handle route.
- Three packages gain an import of `adhocdata` for its vocabulary, which
  makes an app-facing package a dependency of lower layers.

### Neutral

- The threat model is unchanged (SD6). This ADR does not make sealed data
  safe against a local adversary; it makes it hard to send somewhere it was
  never meant to go.
- The sensitivity label is one axis of R6's two. Execution mode — the other
  axis — still has no shared type, and still waits for a second consumer.

## Status

Proposed 2026-07-26. **Not implemented.**

Sequenced so the subtraction lands before the addition, and so the probe —
the only part with a new runtime cost — is separable:

1. SD1 vocabulary, SD2 retirement. Net negative lines; no behaviour change,
   since the retired path has no consumer.
2. SD3 label and SD4 refusals. Behaviour changes: a confined run pinned at
   a non-local endpoint is refused with a reason instead of failing at the
   server.
3. SD5 proof. Behaviour changes again: a confined run may reach a non-local
   engine that has demonstrated it can reach this plane.

Landing (1) warrants a dated Update on
[ADR-0134](./0134-adhoc-datasets.md), whose §SD3 revision recorded the pipe
route as retirable; this ADR is the retirement.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0134](./0134-adhoc-datasets.md) — ad-hoc datasets; §SD1–SD3 and the 2026-07-20 query-path revision this ADR completes.
- [ADR-0144](./0144-query-engine-adapters.md) — the engine roles; the `Extra` escape hatch removed here.
- [ADR-0141](./0141-play-endpoint-dispatch-seam.md) — the dispatch seam the resolver-side refusal lives in.
- [ADR-0094](./0094-keelson-introspection-tables.md) — the loopback plane, its `/table` and `/query` endpoints.
- [doc/explanation/query-system-requirements.md](../explanation/query-system-requirements.md) — R2, R3, R6, and the E6 / E9 extension points.
