---
type: explanation
audience: callsites package maintainer
status: draft
---

> **Status: draft — pre-human-review.**

# Callsites — background and cost model

Why a dispatch survey exists, what the classification vocabulary means, and
where its cost model comes from. The decision record is
[ADR-0107](../../../doc/adr/0107-callsites-compiler-adjudicated-dispatch-classification.md);
this page carries the background that ADR builds on.

## The question

Generic instantiations and interface calls are not free, and the cost is
uneven: some type arguments cost nothing (the compiler stencils a dedicated
instantiation), others route method calls through a runtime dictionary and
defeat devirtualization. A governance survey answers *where* each case occurs
so the hot paths can be audited against the known DO/DON'Ts below.

## Two layers

The analyzer classifies statically (what the code says); the adjudication
layer parses `go build -gcflags=-m` (what the toolchain did) and joins the
`devirtualizing …` / `inlining call to …` lines onto call sites by
file:line:column-of-lparen. The layers never override each other:
"classified dynamic, compiler devirtualized" is exactly the kind of finding
the tool exists to surface. The `-m` text is versioned knowledge — the
adjudication test in `gov_callsites_test.go` is the tripwire when a toolchain
bump changes wording or verdicts (the compiler's schema-versioned `-json`
channel was evaluated and carries neither per-callsite inlining nor
devirtualization records; kill-reason in ADR-0107 §Alternatives).

## Shape cost model

Measured on the pinned toolchain (go 1.26.4) by instantiating a generic
identity function and dumping `go tool nm … | grep go.shape`:

- pointer type arguments collapse into **one** shared `go.shape.*uint8`
  instantiation — dictionary-mediated, devirtualization lost;
- structs stencil **per layout** (two same-layout structs share, a
  different-layout one does not);
- `[]int` vs `[]string`, maps, chans, arrays, funcs: each gets its own shape,
  same treatment as `int` and `string`.

Hence the four-value `ShapeClassE` axis (Stenciled / Pointer / Interface /
TypeParam) instead of a syntactic kind enum: syntactic kind does not predict
cost; the earlier `SliceBasic`/`SliceGeneric` and struct-as-dictionary
buckets encoded Go 1.18-era folklore the current compiler contradicts.

## External references

Devirtualization:

* Implementation: https://go.dev/src/cmd/compile/internal/devirtualize/devirtualize.go
* Overview article: https://www.polarsignals.com/blog/posts/2023/11/24/go-interface-devirtualization-and-pgo

Monomorphization:

* Definition: https://en.wikipedia.org/wiki/Monomorphization
* Generics-cost article the DO/DON'Ts derive from (Go 1.18-era; its
  pointer/interface findings still hold per the measurement above, its
  slice-of-basic emphasis no longer does):
  https://planetscale.com/blog/generics-can-make-your-go-code-slower
* Monomorphizer in CockroachDB: https://github.com/cockroachdb/cockroach/blob/master/pkg/sql/colexec/execgen/execgen.go

Condensed DO/DON'Ts (from the planetscale article, annotated):

> DO use generics in data structures — type-safe unboxed storage is their
> best use case.

> DO parametrize functional helpers by their callback types; the compiler may
> flatten them.

> DO NOT attempt to use generics to de-virtualize or inline method calls on
> pointer arguments: all pointers share one gcshape and method information
> lives in the runtime dictionary. *(Still true — this is `ShapeClassPointer`.)*

> DO NOT pass an interface to a generic function: instead of de-virtualizing
> you add a second indirection layer. *(Still the worst case — this is
> `ShapeClassInterface` and the `--fail-on interface-type-arg` rule.)*

## Coverage honesty

Build constraints legitimately exclude files, so a survey is always
per-configuration: the loader reports constraint-excluded file counts
(`LoadStats.IgnoredFiles`) and the CLI warns when the count is non-zero,
because a hollow survey that looks complete is worse than an error
(ADR-0107 §SD5).
