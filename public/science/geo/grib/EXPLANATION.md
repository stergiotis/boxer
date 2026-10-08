---
type: explanation
audience: whoever changes the decoding arithmetic of this package or reads it to check a value
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# How a GRIB value is reconstructed, and why the order of operations is fixed

These properties hold for any implementation of the format; the decisions
about which templates to read and what to refuse are in ADR-0292.

## The value formula

Every grid-point packing codes a value as an integer `X` and recovers it as

    value = (R + X · 2^E) · 10^(−D)

with `R` the reference value (an IEEE single), `E` the binary scale factor
and `D` the decimal scale factor, both sign-magnitude 16-bit integers. `E`
scales the integer's step: `2^E` is exact in binary floating point. `D`
shifts the decimal point: `10^(−D)` is not exact, and the result depends on
whether it is applied as a multiplication by `10^(−D)` or a division by
`10^D`, and on whether the reference is added before or after. The two
orders differ in the last bit for about one value in ten. This package
multiplies by `math.Pow(10, −D)` after adding the reference, which is the
order that reproduces the reference implementation's doubles bit for bit
across the fixtures; the digest lane holds it there. A negative `D`
multiplies by a power of ten (JMA codes pressure in Pa with `D = −2`), and
a `D` with `X` absent — bits per value zero — still applies: the constant
field is `R · 10^(−D)`, not `R`.

## Complex packing

Templates 5.2 and 5.3 split the `X` values into `NG` groups. Each group has
a reference `r_g` at the field's bit width, a width `w_g` (a reference plus
a small coded increment) and a length `l_g` (a reference plus a scaled
increment, except the last group whose true length is coded separately).
The data section holds, in order and each run padded to an octet: the `NG`
references, the `NG` width increments, the `NG` length increments, then the
values group by group at their group's width, so that `X = r_g + x`. A
group of width zero has every value equal to its reference and occupies no
bits, which is how a run of equal values costs one reference.

Missing-value management (code table 5.5) reserves the all-ones pattern at
the group's width for a primary missing value, and the pattern below it for
a secondary one; a group of width zero whose reference is all ones at the
field's width is entirely missing. Consequently a group reference of zero
is not a constant field, and a width of zero is not a missing group — both
have been misread.

Template 5.3 codes not `X` but its first or second differences. The section
begins with the first one or two original values and the minimum of the
differenced series, at a coded octet width, the minimum sign-magnitude; the
group-decoded integers are differences shifted by that minimum, and the
series is rebuilt as `X_i = X_{i−1} + d_i` or `X_i = 2X_{i−1} − X_{i−2} +
d_i`. Missing values take no part: the recurrence runs over the values that
are present, in stored order. A reader that differences across a missing
point drifts from there to the end of the field.

## Scan flags and stored order

Flag table 3.4 has four independent bits: the direction of the first row
(`i`), the direction of successive rows (`j`), whether consecutive stored
values are adjacent in `i` or in `j`, and whether alternate rows reverse
direction. Sixteen combinations are legal and several are in use (NDFD and
NBM alternate rows). The stored order is the only order the bytes have;
every other order is a view that must apply all four bits exactly once. A
reader that applies them for the caller, and a caller that applies them
again, together flip a field silently — the disagreement between two
independent decoders found in the survey behind ADR-0292.

## Rotated latitude/longitude

Template 3.1 gives the geographic position of the rotated system's south
pole `(φ_sp, λ_sp)` and a rotation angle. The rotated north pole is the
antipode `(φ_p, λ_p) = (−φ_sp, λ_sp + 180°)`, and the rotated origin lies
90° north of the rotated south pole along its meridian, at geographic
`(90° + φ_sp, λ_sp)`. A rotated point `(φ′, λ′)` maps to

    φ = asin( sin φ′ sin φ_p + cos φ′ cos λ′ cos φ_p )
    λ = λ_p − atan2( cos φ′ sin λ′ , sin φ′ cos φ_p − cos φ′ cos λ′ sin φ_p )

which the rotated fixture holds against the oracle's un-rotated
coordinates. The rotation angle is applied to `λ′` before the mapping;
this package refuses a non-zero angle until a fixture exists for its sign.

## Gaussian latitudes

A Gaussian grid with `N` circles between a pole and the equator has its
`2N` latitudes at the arcsines of the roots of the Legendre polynomial
`P_2N`. The roots are found by Newton's method from the Chebyshev estimate
`cos(π(i + 3/4)/(2N + 1/2))`, with `P_2N` and its derivative from the
three-term recurrence; the roots are symmetric about zero, so half are
solved. A reduced Gaussian grid places `PL[j]` points on circle `j`,
spaced `360°/PL[j]` from the first longitude; the grid is global when the
row counts sum to the point count and the last longitude sits one increment
of the longest row short of the first.

## The adaptive entropy coder (template 5.42)

CCSDS 121.0-B-3 codes the packed integers block by block, `J` samples at a
time, each block under the option that made it shortest. The decoder needs
only the parameters the transport carries: the sample resolution `n`, the
block size `J`, the reference sample interval `r`, whether the preprocessor
ran, and whether the restricted option set is in use. Each coded data set
opens with an identifier whose length depends on `n` (3 bits up to 8, 4 up
to 16, 5 up to 32; 1 or 2 bits in the restricted set), then an uncoded
`n`-bit reference sample if the block opens a reference interval and the
preprocessor is on, then the block:

- **Split-sample `k`** (identifier `k + 1`; `k = 0` is the fundamental
  sequence): the `J` values' upper bits as unary codewords — `m` zeros and a
  one for the value `m` — all first, then the `k` low bits of each.
- **Second extension** (identifier zero, then a one bit): `J/2` unary
  codewords, each the pair transform `γ = (a + b)(a + b + 1)/2 + b`; the
  inverse takes the largest `s` with `s(s + 1)/2 ≤ γ`, whence `b` and `a`.
- **Zero block** (identifier zero, then a zero bit): one unary codeword for
  a run of all-zero blocks, counted within a segment of 64 blocks of the
  interval: 1–4 for that many blocks, 5 for "the rest of the segment", and
  `m ≥ 6` for `m` blocks.
- **No compression** (identifier all ones): the samples at `n` bits.

A block of `J` therefore never needs more than `J` unary codewords, and a
unary codeword longer than the resolution allows is corruption, not a
large value.

The preprocessor is the unit-delay predictor with the prediction-error
mapper: the prediction is the previous sample, the error `Δ = x − x̂` is
mapped by `θ = min(x̂ − x_min, x_max − x̂)` to `δ = 2Δ` for `0 ≤ Δ ≤ θ`,
`δ = 2|Δ| − 1` for `−θ ≤ Δ < 0`, and `δ = θ + |Δ|` beyond, which is only
possible towards the farther bound. The inverse is a case split on `δ ≤ 2θ`
and on which bound is nearer. The mapper is exact for every `x̂`, so a
stream that yields a sample outside `[x_min, x_max]` is corrupt.

Template 5.42's options mask is the flag word its note points to, libaec's:
bits for signed data, three-byte samples and MSB-first byte order describe
the *uncompressed* layout and do not touch the integers; the preprocessor
bit (8), the restricted-set bit (16) and the interval-padding bit (32),
which pads the coded stream to an octet at every reference interval, change
the decoding and are honoured. The synthetic fixtures cover each of them.

## JPEG 2000 (template 5.40)

Template 5.40 wraps a JPEG 2000 Part 1 codestream whose single component
is the field's packed integers at the field's bit width. The standard is
large; the profile producers use is small, and the decoder implements
that profile and names everything else. From ITU-T T.800, the pieces are:

- **Codestream syntax** (Annex A): SIZ, COD/COC, QCD/QCC, the tile-part
  headers, and the optional SOP and EPH markers around packets. One tile,
  one component, no sub-sampling.
- **Ordering** (Annex B): the resolution levels and sub-bands of (B-15),
  the code-block partition anchored at the origin, and packet headers
  whose inclusion and zero-bit-plane information are tag trees, whose pass
  counts follow Table B.4, and whose segment lengths grow with `Lblock`.
  With one layer and one precinct per resolution, every progression order
  reduces to resolution 0 upwards, which is why the decoder accepts all
  five while refusing a second layer or a precinct partition.
- **The MQ decoder** (Annex C): the software-conventions decoder with its
  47-state probability table and the 0xFF stuffing rule; a segment is
  extended with 0xFF bytes when the passes outrun it (D.4.1).
- **Coefficient bit modelling** (Annex D): the first bit-plane in a cleanup
  pass, then significance propagation, magnitude refinement and cleanup
  per plane over stripes of four rows; the nine significance contexts per
  orientation, five sign contexts with their XOR, three refinement
  contexts, the run-length and uniform contexts. Context reset, vertically
  causal formation and segmentation symbols are honoured; the bypass and
  per-pass termination modes change the segment structure and are refused.
- **Reconstruction** (Annex E): `Mb = G + ε_b − 1` bit-planes per sub-band
  from the guard bits and the exponent; with the reversible wavelet and
  every pass present the integers are exact, and a truncated code-block is
  placed at the middle of its last decoded interval.
- **The 5/3 inverse wavelet** (Annex F): 2D_SR interleaves the four
  sub-bands into the next lower LL band's absolute coordinates and runs
  the two lifting steps over rows then columns with periodic symmetric
  extension, parity taken from the absolute coordinate, so an odd origin
  or an odd length reconstructs as the encoder intended.
- **DC level shift** (Annex G): `2^(precision − 1)` is added back for
  unsigned samples.

The scaling then follows the value formula. The reader checks the image's
sample count against the field's and its precision against Section 5's
bit width, and refuses a disagreement rather than reading past it.

## Edition 1

An edition 1 message is four sections after the 8-octet indicator: the
product definition (identity, level, time, decimal scale, and from octet
41 the centre's local use), the optional grid description, the optional
bitmap, and the binary data, ending in the same `7777`. The same
quantities exist as in edition 2 and the reader maps them onto the same
types: representation type 0 is template 3.0, 4 is 3.40, 10 is 3.1;
coordinates are millidegrees rather than microdegrees, so the rounding of a
coded increment over a long row can miss the corner by tens of
millidegrees, and the corners take precedence when they agree with the
increment to within one unit.

Three things edition 1 does not code. **The point count**: a regular grid's
is `Ni × Nj`; a reduced grid's is the sum of its row lengths — except that
ECMWF codes a sub-area of a reduced Gaussian grid with the *global* row
lengths, so the count has to come from the data: the bitmap's bit count,
or the packed bits divided by the width. **A missing marker**: the bitmap is
the only way to say a point has no value, and DWD's convention for a whole
field with no values is a binary scale factor and a reference value of all
ones, which the reference implementation decodes as every point missing
and this reader follows. **A second per-message identity**: local
definitions are the centre's; ECMWF's definition 1 carries the ensemble
member and the reader decodes it, and nothing else.

The reference value is an IBM System/360 single: a sign bit, a seven-bit
exponent in excess 64 to base 16, and a 24-bit fraction, so
`v = (−1)^s · 16^(e − 64) · f / 2^24`. It has 24 bits of precision where
IEEE single has 23, which is why the reader carries the reference as a
double for both editions: converting through a single would lose the last
bit of every edition 1 field.

## Edition 1 message length

Edition 1 codes the total length in 24 bits. ECMWF's convention for longer
messages sets the top bit and stores the length in units of 120 bytes; the
data section's own length field, then under 120, carries what to subtract:
`total = units · 120 − codedDataLength + 4`. A reader taking the field at
face value ends the message early and never finds the end marker.
