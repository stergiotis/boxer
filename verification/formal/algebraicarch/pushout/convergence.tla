---------------------------- MODULE convergence ----------------------------
\* Liveness of the boxer pushout exchange protocol, checked with TLC.
\*
\* TLC-native companion to convergence.qnt, kept in LOCKSTEP with it: identical
\* Nodes / Patches / deps / origin and identical Record / Offer / Deliver
\* actions. The Quint file is the readable source and carries a bounded
\* witness (`quint test`); this file carries the actual liveness proof, because
\* Apalache's liveness checking is impractically slow and TLC is the right tool.
\*
\* Scope: the SAFETY of the exchange protocol (loss / reorder / dup / partial
\* sync) is in pushout_exchange.qnt. This model assumes a RELIABLE carrier and
\* proves PROGRESS: under fair scheduling every recorded patch reaches every
\* node, so all repos converge.
\*
\* Refinement: Record -> repo.Repo.Record (repo/repo.go:230); Offer ->
\* exchange.Push/Pull diff (exchange/exchange.go); Deliver ->
\* repo.Repo.ApplyEnvelope (repo/repo.go:275, idempotent + dependency-gated).

EXTENDS Integers, FiniteSets, TLC

\* Fixed model (mirrors convergence.qnt). 2 and 3 each depend on 1 and are
\* independent of each other; each patch is authored once at its origin.
Nodes   == {"a", "b", "c"}
Patches == {1, 2, 3}
deps    == (1 :> {} @@ 2 :> {1} @@ 3 :> {1})
origin  == (1 :> "a" @@ 2 :> "b" @@ 3 :> "c")

VARIABLES applied, inflight
vars == <<applied, inflight>>

\* author p at its origin, once, when its declared deps are applied there
Record(p) ==
    /\ p \notin applied[origin[p]]
    /\ deps[p] \subseteq applied[origin[p]]
    /\ applied' = [applied EXCEPT ![origin[p]] = @ \union {p}]
    /\ inflight' = inflight

\* perpetual re-offer: ship a patch the holder has and the target lacks
\* (Push/Pull recompute the diff each run). Reliable: stays until delivered.
Offer(n, m, p) ==
    /\ n /= m
    /\ p \in applied[n]
    /\ p \notin applied[m]
    /\ <<m, p>> \notin inflight
    /\ inflight' = inflight \union {<<m, p>>}
    /\ applied' = applied

\* apply an in-flight envelope: idempotent, dependency-gated; consumes it
Deliver(m, p) ==
    /\ <<m, p>> \in inflight
    /\ deps[p] \subseteq applied[m]
    /\ applied' = [applied EXCEPT ![m] = @ \union {p}]
    /\ inflight' = inflight \ {<<m, p>>}

Init ==
    /\ applied = [n \in Nodes |-> {}]
    /\ inflight = {}

Next ==
    \/ \E p \in Patches : Record(p)
    \/ \E n, m \in Nodes, p \in Patches : Offer(n, m, p)
    \/ \E m \in Nodes, p \in Patches : Deliver(m, p)

\* Weak fairness on every action instance: an action that stays enabled is
\* eventually taken (no enabled sync is starved forever).
Fairness ==
    /\ \A p \in Patches : WF_vars(Record(p))
    /\ \A n, m \in Nodes, p \in Patches : WF_vars(Offer(n, m, p))
    /\ \A m \in Nodes, p \in Patches : WF_vars(Deliver(m, p))

Spec       == Init /\ [][Next]_vars /\ Fairness
SpecNoFair == Init /\ [][Next]_vars

\* ---- predicates -----------------------------------------------------------
Converged       == \A n, m \in Nodes : applied[n] = applied[m]
FullyReplicated == \A n \in Nodes : \A p \in Patches : p \in applied[n]
\* safety sanity: every repo stays dependency-closed (Deliver is gated)
DepClosed == \A n \in Nodes : \A p \in applied[n] : deps[p] \subseteq applied[n]

\* THE liveness result: under fairness, the system eventually reaches and stays
\* at full replication (hence convergence). Checked under Spec (holds) and,
\* via convergence_nofair.cfg, under SpecNoFair (fails -> fairness is required).
Convergence == <>[]FullyReplicated
=============================================================================
