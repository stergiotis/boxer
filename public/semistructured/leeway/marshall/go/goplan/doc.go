// Package goplan is the Go-DTO front-end toolkit for the leeway marshalling
// stack: the machinery that turns an annotated Go struct into a validated
// mappingplan.Plan, plus the shared classification both codec back-ends read.
//
// It sits one layer above the mappingplan model (which it imports) and one
// below the two front-ends (which import it). You need goplan only to PRODUCE
// or drive a Plan; consuming a finished Plan needs the mappingplan model alone,
// which is why the ClickHouse readback generator and the codegen
// WrapperEmitterI implementations depend on mappingplan and not on goplan.
//
// # Front-end-authoring API
//
// What a new front-end — a sibling of marshallgen (go/ast) and marshallreflect
// (reflect) — builds on:
//
//   - SplitLW / ParsedLWTag and the tuple-grammar parsers (SplitTupleOuterLW,
//     SplitTupleElemLW): the lw: tag grammar, one shared flag vocabulary.
//   - PlanBuilder (NewPlanBuilder; AddField / AddUnderscoreField /
//     AddTupleSliceField / AddNestedSliceField; Finish): the per-field
//     validation + whole-DTO assembly shared by both front-ends, so the
//     go/ast and reflect paths cannot drift on what they accept — the parity
//     corpus (marshallreflect_test) gates the two accept sets mechanically.
//   - FieldShape: the front-end-agnostic value-type classification a front-end
//     fills in per field.
//   - ScalarCanonicalForGoType: the Go→canonical half of the classifiers.
//   - ValidatePlainColumnShape: the plain-column role + Go-type check.
//
// # Codec-shared helpers
//
// Exported so the generated codec (marshallgen's emit) and the reflect codec
// (marshallreflect) share one source of truth — not a front-end-authoring
// surface: ComputeGroups / SectionGroup / SubColumn, ClassifyBegin /
// FieldBeginShape, SingleValueReadAccessor, CopyStrategy / CopyStratE,
// PlainArrowArrayType / IsSupportedPlainType, FixedByteArrayLen /
// IsFixedByteArray, RoaringElemCanonical, FindPlainCol.
//
// # Stability
//
// The front-end-authoring API is stable for front-end authors. The
// codec-shared helpers carry a narrower promise: they track the back-ends'
// shared needs and may change with them. The mappingplan model is the broad,
// frozen surface — prefer it for anything that only reads a Plan.
package goplan
