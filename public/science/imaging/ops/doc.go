// Package ops computes new images from images (ADR-0400): a declared
// operator applied to named inputs with stated parameters, run once at full
// resolution into a tiled store, with a record of what it did. Nothing is
// evaluated at a reduced level; the store's levels are reduced from the
// result's own samples, so what a viewer shows at any zoom is the reduction
// of the result.
//
// # Operators
//
// An operator is declared once (§SD1): a [Declaration] holds its id, its
// version, a summary, the number of inputs it takes, whether it leaves
// invalid samples out, and its parameters, each with a type, a unit, a
// range, choices, a default or none, and a doc sentence. A dialog, a
// catalog's arguments, [Declaration.Help] and the record are read from it,
// and [Declaration.Validate] turns given values into the canonical form a
// record and a request's identity use; [Declaration.CheckParam] judges one
// field's value as it is typed. [Register], [Lookup] and [All] keep
// the operators; five are registered by this package (§SD10):
//
//   - expr — one plane from an expression over named single-plane inputs.
//   - axis-stat — the mean, median, minimum, maximum, standard deviation or
//     count of each pixel's valid samples across the inputs.
//   - gaussian — a Gaussian smoothing, truncated at ⌈4σ⌉ pixels.
//   - median-filter — the median over a square window.
//   - standardize — (value − mean) / standard deviation, per plane, with the
//     plane's own statistics from a first pass.
//
// An operator's author provides an [OperatorI]: the declaration, and a plan
// made from the inputs' descriptions and the validated parameters that
// states the output's colour model, kind and plane labels, the halo of
// input rows and columns a band needs and the border rule for it, the
// number of first passes, and notes (§SD3), or a refusal wrapping
// [ErrRefused] that names its reason. With the plan comes a [KernelI],
// whose first passes see the inputs band by band and whose workers compute
// one output row at a time from a [Band] of values. The runner fills in
// the size, the identity transform and the placement, reads, pads, cuts
// bands, writes the store and the record; a kernel reads its inputs and its
// parameters and nothing else.
//
// # Values, invalid samples and the border
//
// A kernel sees values (§SD4): each stored sample through its plane's value
// transform, as a float64, with NaN for every invalid sample — NaN in a
// float plane and the value a plane declares invalid alike. The output is
// 32-bit float under the identity transform when every input kind is exact
// in a float32 (u8, u16, i16, f32) and 64-bit float otherwise (i32, f64);
// nothing wraps or saturates. Its invalid samples are NaN, one NaN with one
// bit pattern, and it declares no invalid value (§SD5). An output sample
// that depends on an invalid input sample is invalid, unless the operator
// declares that it leaves invalid samples out, as axis-stat does; a value
// that is not finite — computed, or a float32 overflow — is written as
// invalid. An infinite input sample is valid, and every arithmetic result
// that is infinite is not.
//
// A neighbourhood operator takes a required border parameter with no
// default (§SD6): under "invalid" samples outside the image do not exist,
// the halo around the image holds NaN, and every output sample whose
// neighbourhood reaches outside the image is invalid; under "nearest" the
// nearest edge sample stands in for each outside one.
//
// # Running
//
// [Run] waits for the inputs' samples, validates and plans, makes the
// first passes, then computes the output top to bottom in bands of rows —
// 512 by default — reading each band's inputs with their halo, and hands
// the store to OnStore before the first band (§SD9); Progress counts the
// rows of the first passes before the output's, so a run that scans first
// is seen to move while it scans. It honours
// cancellation between bands; a cancelled or failed run fails the store
// and returns no record. Rows of a band are computed on several goroutines,
// each row by one of them and every sample by the same arithmetic in the
// same order, so that neither the band size nor the scheduling changes a
// sample. A first pass reduces in row-major order, plane by plane, with
// compensated sums, so two runs of one request find the same quantities
// (§SD7).
//
// An input is an image in memory ([FromImage]), a store, or a plane of a
// stack taken at stated positions ([StackInput]), which the record names.
// Inputs are on one grid (§SD2): a request whose inputs differ in size is
// refused, naming them. The result keeps the inputs' placement when they
// all have the same one and has none otherwise, with a note.
//
// # Records
//
// A [Record] is what an operation did (§SD8): the operator and version, the
// parameters as given and canonical, each input's name, digest, value
// transforms, declared invalid value and stack positions, the records of
// inputs that are results themselves, the first-pass quantities and the
// output's digest. Its JSON document ([Record.Encode], [DecodeRecord]) is
// versioned by [RecordSchema] and decoded strictly. [RequestIdentity] is
// known before a request runs. A result's provenance names the operation —
// format "operation", decoder "<id>@<version>" and a note with the
// parameters — and [RecordOf] returns the full record of a result's store
// for as long as the store is held. [Replay] runs a record's request again
// and reports whether the output's digest is the recorded one.
//
// # What is resident
//
// During a run: the inputs, one band of each input's values as float64
// with its halo, one band of output values, and the store being built.
// Afterwards the store holds the result and its levels; the inputs are the
// caller's to keep or drop.
//
// # The expression grammar
//
// An expression of the expr operator is, in EBNF:
//
//	expression = or ;
//	or         = and { "||" and } ;
//	and        = comparison { "&&" comparison } ;
//	comparison = sum [ ( "<" | "<=" | ">" | ">=" | "==" | "!=" ) sum ] ;
//	sum        = term { ( "+" | "-" ) term } ;
//	term       = unary { ( "*" | "/" ) unary } ;
//	unary      = ( "-" | "!" ) unary | primary ;
//	primary    = number | name | call | "(" expression ")" ;
//	call       = function "(" expression { "," expression } ")" ;
//	function   = "abs" | "sqrt" | "exp" | "log" | "log10" | "pow" | "min"
//	           | "max" | "clamp" | "where" | "isvalid" ;
//	name       = ( letter | "_" ) { letter | digit | "_" } ;
//	number     = ( digits [ "." { digit } ] | "." digits ) [ exponent ] ;
//	exponent   = ( "e" | "E" ) [ "+" | "-" ] digits ;
//	digits     = digit { digit } ;
//
// Spaces, tabs and line breaks separate tokens. Comparisons do not chain.
// A function takes a fixed number of arguments: one for abs, sqrt, exp,
// log, log10 and isvalid, two for pow, min and max, three for clamp(x, lo,
// hi) — min(max(x, lo), hi) — and where(condition, a, b). A comparison, &&,
// || and ! give 1 or 0, and a non-zero operand is true. A name is an
// input's; nothing else is reachable — no assignment, no loop, no host.
//
// An invalid operand makes every operation invalid except two: isvalid(x)
// is 1 for a valid x and 0 for an invalid one, and where(c, a, b) is a
// where c is valid and non-zero, b where c is zero, each with its own
// validity, and invalid where c is. Division by zero, the logarithm or
// square root of a value outside their domain, and every other result
// that is not finite are invalid. An expression is parsed once and
// compiled to a flat program that runs over whole rows; a text that does
// not parse is a [*ParseError] naming the column and what was expected.
// It is held in a record in its canonical spelling ([Expression.String]).
// At most 4096 bytes, 1024 terms and 64 levels of nesting are read.
package ops
