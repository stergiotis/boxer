------------------------- MODULE replay_liveness -------------------------
(* Liveness of the suspend/replay protocol (public/caching WorkItem /     *)
(* IterateRestWorkItems): does a flush-until-quiet replay loop QUIESCE?   *)
(*                                                                        *)
(* The machine: work items are discovered (their missing keys queue and   *)
(* the item suspends), flushes fetch queued keys from the upstream (keys  *)
(* the upstream lacks are marked absent when NegCache is on — the         *)
(* WithNegativeCaching option), and pending items replay: complete when   *)
(* their needs are cached, re-queue what is still queueable, or — when    *)
(* every missing key is absent-marked — leave the pending set for good    *)
(* (an absent-marked miss neither queues nor suspends).                   *)
(*                                                                        *)
(* The theorem pair (the R10 finding as liveness):                        *)
(*   - NegCache = TRUE:  Quiescence holds — even for items needing keys   *)
(*     the upstream does not have. "No error has been found."             *)
(*   - NegCache = FALSE: Quiescence is VIOLATED despite full weak         *)
(*     fairness: an item needing an absent key re-queues on every replay, *)
(*     forever — fairness does not save you, only negative caching does.  *)
(*     TLC prints the livelock lasso.                                     *)
(* SatisfiableDone holds in BOTH modes: items whose needs the upstream    *)
(* can serve always complete; the livelock never starves them.           *)
EXTENDS Naturals, FiniteSets, TLC

CONSTANTS Keys, Items, needs, upstream, NegCache

VARIABLES cached, fetchq, pending, absent, done

vars == <<cached, fetchq, pending, absent, done>>

\* Concrete instances for the .cfg files (needs/upstream are functions,
\* which TLC configs cannot spell inline).
CONSTANTS i1, i2, k1, k2
NeedsDef == i1 :> {k1} @@ i2 :> {k1, k2}
UpstreamDef == {k1}

Missing(i) == needs[i] \ cached
Queueable(i) == Missing(i) \ absent

Init ==
  /\ cached = {} /\ fetchq = {} /\ absent = {}
  /\ pending = {} /\ done = {}

\* Discovery: the item's first pass. Completes directly when everything is
\* cached; suspends (queue + pending) when something queueable is missing;
\* is DISABLED when every missing key is absent-marked — the miss neither
\* queues nor suspends (getInternal's negative-cache path).
Discover(i) ==
  /\ i \notin done
  /\ i \notin pending
  /\ IF Missing(i) = {}
     THEN /\ done' = done \cup {i}
          /\ UNCHANGED <<cached, fetchq, pending, absent>>
     ELSE /\ Queueable(i) # {}
          /\ fetchq' = fetchq \cup Queueable(i)
          /\ pending' = pending \cup {i}
          /\ UNCHANGED <<cached, absent, done>>

\* One batched fetch: present keys land in the cache, missing ones are
\* absent-marked iff negative caching is on (performFetch's clean-return
\* marking).
Flush ==
  /\ fetchq # {}
  /\ cached' = cached \cup (fetchq \cap upstream)
  /\ absent' = IF NegCache THEN absent \cup (fetchq \ upstream) ELSE absent
  /\ fetchq' = {}
  /\ UNCHANGED <<pending, done>>

\* Replay of a pending item (yieldPending + the idempotent user body):
\* completes, re-queues what is still queueable and stays pending, or —
\* all missing keys absent — leaves the pending set without completing.
Replay(i) ==
  /\ i \in pending
  /\ IF Missing(i) = {}
     THEN /\ pending' = pending \ {i}
          /\ done' = done \cup {i}
          /\ UNCHANGED <<cached, fetchq, absent>>
     ELSE IF Queueable(i) # {}
     THEN /\ fetchq' = fetchq \cup Queueable(i)
          /\ UNCHANGED <<cached, pending, absent, done>>
     ELSE /\ pending' = pending \ {i}
          /\ UNCHANGED <<cached, fetchq, absent, done>>

Next ==
  \/ \E i \in Items : Discover(i) \/ Replay(i)
  \/ Flush

\* Weak fairness on every action: an enabled flush or replay is never
\* starved. The NegCache = FALSE violation happens DESPITE this.
Fairness ==
  /\ WF_vars(Flush)
  /\ \A i \in Items : WF_vars(Discover(i)) /\ WF_vars(Replay(i))

Spec == Init /\ [][Next]_vars /\ Fairness

--------------------------------------------------------------------------

\* Eventually the loop is quiet forever: nothing pending, nothing queued.
Quiescence == <>[](pending = {} /\ fetchq = {})

\* Items the upstream can fully serve always complete — the livelock (when
\* it exists) never starves satisfiable work.
Satisfiable(i) == needs[i] \subseteq upstream
SatisfiableDone == <>[](\A i \in Items : Satisfiable(i) => i \in done)

=============================================================================
