// Package marshallreflect is the runtime-reflection sibling of
// boxer/public/semistructured/leeway/marshall/go/marshallgen. Both build on
// the shared Plan IR (mappingplan) and the Go-DTO construction machinery
// (goplan): marshallreflect parses the same `lw:` tag vocabulary at runtime
// via reflect.StructTag (no go/ast), produces the same mappingplan.Plan
// value, and drives a Go DTO ↔ leeway-DML chain through reflect.Value
// method dispatch.
//
// Use cases (per the slow-path / config-store rationale):
//
//   - Marshalling DTOs whose code is not pre-known to a generator
//     pass (config files, ad-hoc tooling, dynamic schemas).
//   - Tests that exercise multiple DML implementations against the
//     same DTO without regenerating per case.
//
// Hot-path uses must continue to call marshallgen-generated
// BuildEntities — the reflect path pays per-row reflection costs
// (method lookup, value boxing).
//
// Wire compatibility is the load-bearing invariant: the bytes emitted
// by marshallreflect.Marshal(rows) followed by dml.TransferRecords
// must equal the bytes emitted by marshallgen's generated
// <Kind>BuildEntities(dml, columns) followed by dml.TransferRecords,
// for the same DTO. Verified via round-trip tests against a
// recording mock DML and (transitively) against the typed DMLs that
// already round-trip in the per-kind keelson codec test suites.
//
// The membership-id resolver is pluggable via LookupI — pebble's
// facts target wraps vdd.KeelsonHrNkRegistry; an anchor or schema-
// agnostic target can use NoLookup if every membership in its DTOs
// carries `,verbatim`.
//
// # Round-trip recipe
//
// Write side — marshal rows into a leeway DML, then drain to Arrow records:
//
//	dml := schema.NewMyTable(allocator, len(rows)) // a generated leeway DML
//	if err := marshallreflect.Marshal(dml, rows, lookup); err != nil { … }
//	recs, err := dml.TransferRecords(nil)          // wire bytes live in the records
//
// Read side — bind each section's read-access reader to the record, register
// them, then unmarshal:
//
//	idR := schema.NewReadAccessMyTablePlainEntityIdAttributes()
//	idR.SetColumnIndices(idR.GetColumnIndices()); _ = idR.LoadFromRecord(recs[0])
//	symR := schema.NewReadAccessMyTableTaggedSymbol()
//	symR.SetColumnIndices(symR.GetColumnIndices()); _ = symR.LoadFromRecord(recs[0])
//
//	readers := marshallreflect.NewSectionReaders(idR.Len()).
//		PlainColumn("id", idR.ValueId).
//		Section("symbol", symR.GetAttributes(), symR.GetMemberships())
//	var out []MyDTO
//	err = marshallreflect.Unmarshal(readers, &out, lookup)
//
// The same mappingplan.Plan drives both directions, so the bytes round-trip (and
// equal what marshallgen's generated <Kind>BuildEntities / <Kind>FillFromArrow
// produce). Validate[T](dml) preflights the write contract; SectionReaders'
// up-front coverage check reports any plain column / section the readers omit.
// Worked end-to-end examples live in marshallreflect_test/shapes_roundtrip_test.go.
//
// # The DML write contract
//
// Marshal / RowComposer drive `dml` (passed as any) by reflected method
// dispatch; the method set below IS the contract. A missing or mis-typed method
// otherwise panics mid-marshal (mustCall); Validate[T](dml) preflights the whole
// set and reports every mismatch in one error before the first row.
//
// Entity frame, on dml:
//   - BeginEntity() — open an entity.
//   - SetId(id), or SetId(id, naturalKey) — two args iff a naturalKey plain column is declared.
//   - SetTimestamp(ts) — only if a `ts` plain column is declared.
//   - SetLifecycle(expiresAt) — only if an `expiresAt` plain column is declared.
//   - GetSection<X>() Sec — one per section; X = UpperFirst(section name).
//   - CommitEntity() [error] — close the entity; a returned error is surfaced.
//
// Plain-column setter arguments are passed verbatim: the DTO field's Go type is
// the setter's argument type (strict 1:1, no conversion).
//
// Section frame, on the Sec from GetSection<X>:
//   - BeginAttribute(v) Attr — scalar value (ShapeScalarBegin / exploded element).
//   - BeginAttributeSingle(v) Attr — scalar value with `,unit`.
//   - BeginAttribute() Attr — container open, no args (default multi shape).
//   - BeginAttribute(s1, s2, …) Attr — multi-sub-column section: one arg per
//     *scalar* sub-column, in declaration order (none when every sub-column is
//     a container). Container sub-columns append via AddTo(Co)Container(s)P on
//     the attribute frame (ADR-0101).
//   - EndSection() — close the section.
//
// Multi-sub-column DTO contract (ADR-0101): one Go field per sub-column —
// scalars as T, containers as []T; within each class the DTO declaration order
// must match the schema's column order. All container fields of one section
// must have equal length per row (co-containers zip element-wise; checked at
// marshal time). With at least one scalar sub-column the attribute always
// emits (empty containers are legal, N = 0); an all-container tuple with every
// container empty is spliced. Option / roaring / `unit` / `explode` / const /
// carrier channels are rejected at plan time in such sections.
//
// Attribute frame, on the Attr from a Begin call:
//   - AddToContainerP(v) — append one container value.
//   - AddToCoContainersP(c1, c2, …) — multi-sub-column co-containers: append
//     one element per container sub-column, zipped (named AddToContainerP when
//     the section has exactly one container sub-column).
//   - AddMembership<Suffix>P(…) — push the membership; the Suffix and argument
//     list are the channel's (see mappingplan.MembershipChannel): Ref →
//     …RefP(id uint64); Verbatim → …VerbatimP(name []byte); MixedLowCardRef →
//     …P(id uint64, params []byte); MixedLowCardVerbatim → …P(name []byte,
//     params []byte); *Parametrized → …P(params []byte).
//   - EndAttributeP() — close the attribute.
//
// # The RA read contract
//
// Unmarshal reads through the per-section attribute + membership readers the
// caller registers via a SectionReaders builder (NewSectionReaders +
// PlainColumn / Section). Index arguments are raruntime.EntityIdx (int) and
// raruntime.AttributeIdx (int64).
//
// Plain columns: the caller returns the Arrow array goplan.PlainArrowArrayType
// maps the field's Go type to — e.g. *array.Uint64 for uint64, *array.Timestamp
// for time.Time, *array.FixedSizeBinary for [16]byte.
//
// Attribute reader, per section:
//   - GetNumberOfAttributes(e) int64 — attribute count for entity e.
//   - GetAttrValueValue(e, a) iter.Seq[T] — container / multi values (also the
//     scalar or exploded-element single value).
//   - GetAttrValueSingleOrDefault(e, a) T — single value for HA / single-slot shapes.
//   - GetAttrValue<Col>(e, a) T — multi-sub-column scalar sub-column accessor;
//     Col = UpperFirst(sub-column).
//   - GetAttrValue<Col>(e, a) iter.Seq[T] — multi-sub-column container
//     sub-column accessor (drained per attribute; an N = 0 attribute reads
//     back as a nil slice).
//
// The single-value accessor is chosen by goplan.SingleValueReadAccessor, so the
// reflect codec and the generated codec cannot diverge on the choice.
//
// Membership reader, per section:
//   - GetMembValue<Suffix>(e, a) — simple channels: iter.Seq[uint64] (Ref) or iter.Seq[[]byte] (Verbatim).
//   - GetMembValue<CarrierReadSuffix>(e, a) — carrier channels: iter.Seq2[…] (mixed) or iter.Seq[[]byte] (parametrized).
package marshallreflect
