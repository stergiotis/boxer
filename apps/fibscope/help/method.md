---
type: explanation
audience: end-user
status: draft
title: How a tagged id is built
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# How a tagged id is built

The scheme is ADR-0106's: fibonacci-coded tags are the only identifier
scheme. Four packages under `public/identity` carry it, and the window is a
view over them, so a readout that disagrees with the packages is a defect in
the window:

- `fibonaccicode` — the codec: Zeckendorf representations terminated by a
  `11` comma, MSB-aligned in a `uint64`, with the width conventions and the
  encode bias.
- `identifier` — composing and splitting ids: `TaggedId`, `IdTag`,
  `UntaggedId` and `TagValue`, with `Split`, `IsValid`, `GetTagWidth` and
  `GetMaxPossibleIdIncl`.
- `fibonacci` — planning: `WidthClasses` and `SelectFittingTagValueRange`,
  which the Trade-offs tab draws.
- `identsql` — the ClickHouse surface: the `LW_ID_*` macro family as a
  nanopass expansion pass, `ExpandPass`, and the equivalent `CREATE
  FUNCTION` statements.

The end-to-end recipe — mint, split, query — is the how-to *How to mint,
split, and query fibonacci-tagged identifiers*; this page explains the
readouts only.

## The word

A tagged id is `tagBits | body`. The tag bits are the fibonacci code of
(tag value − 1): a Zeckendorf representation, reversed, terminated by the
`11` comma, placed MSB-aligned; the body occupies the remaining low bits.
Two properties make that work with no out-of-band width. The code is
self-delimiting: scanning from the MSB, the first adjacent `11` pair is
always the tag's comma, because a Zeckendorf representation never has two
adjacent 1s. And it is prefix-free: no tag's code is a prefix of another's,
so ids of distinct tags never collide. Two values are reserved: tag value
0, and body 0 within any tag — bodies are minted from 1.

## The decode readouts

`IsValid` is the structural check — the word carries a comma — and does not
prove the id was minted by a generator. `Split` gives the tag and the body;
on a comma-less word it gives (0, 0) rather than garbage, which is what the
Invalid case shows. The *tag width* is the full width including the comma:
tag value 1 is the bare comma `11`, 2 bits, leaving 62 for the body; the
largest `uint32` tag values cost 47, leaving 17. The width grows with the
value, which is the frequency-adaptive property the tag tooltip means by
giving the hottest categories the smallest values. *max body* is
2^(64 − width) − 1.

## The Zeckendorf sum

Every positive integer is a unique sum of non-consecutive Fibonacci numbers
(Zeckendorf's theorem). The encoder codes tag value − 1 and biases by +1
internally, since 0 has no bare fibonacci code; the two biases cancel, so
the Fibonacci numbers the code's 1-bits stand for sum to the tag value
itself. The *Zeckendorf* readout writes that sum out — the code spells the
value.

## The same-tag range

Within one tag the body fills the low bits from 0 to max body, so the ids of
one tag run contiguously from the tag bits alone (body 0, the reserved
sentinel) to the tag bits with every body bit set. Ids of one tag form a
contiguous UInt64 range, so a tag filter is a plain BETWEEN — sargable and
index-prunable. Prefix-free codes mean different tags never overlap.

## The SQL expansion

The SQL split mirrors the Go split bit for bit: `bitAnd(x, x << 1)` marks
the upper bit of every adjacent `11` pair, the highest such bit is the
comma, and its position is recovered with `roundToExp2` and `bitCount`,
since ClickHouse has no leading-zeros builtin. Invalid ids yield 0 from
every macro, as the Go methods do. `LW_ID_HAS_TAG` with a constant tag
value — the window's case — folds at expansion time into a `BETWEEN` over
that tag's range, which primary-index analysis can prune; no bit arithmetic
survives into the query. The constant-tag filter folds to a sargable range
at expansion time (identsql.ExpandPass) — the same split the Go code does,
golden-locked against ClickHouse. The window runs the pass on `SELECT
LW_ID_HAS_TAG(id, <tag value>) FROM t` and shows the rewritten statement.

## The advisor and the stats table

`SelectFittingTagValueRange(maxExpectedIds)` returns the half-open
tag-value interval whose codes have exactly the largest full width that
still leaves room for `maxExpectedIds` distinct bodies in the word. Small
expectations are clamped to width 46, one below the `uint32` maximum, so
the advice never lands on the top class; an expectation of 2^60 or more is
rejected, since it would need a tag narrower than the 2-bit minimum, and
the slider stops just short of it so the readout stays populated.

The stats table is `WidthClasses`: for each width from 2 to 47, the tag
values whose codes have exactly that width, their count, the largest body
under them, and the encoding overhead — the width divided by the bits a
plain binary encoding of the class's largest value would take, the price of
self-delimitation. Width 47 is clamped at the `uint32` boundary, so its
class is smaller than the mathematical one. The exhaustion columns divide
max body by a steady rate of ids per second; they assume every body is used
and the rate never varies, and are there for scale: Narrow tags are
effectively inexhaustible (giga-years); a wide tag at MHz rates fills in
seconds.
