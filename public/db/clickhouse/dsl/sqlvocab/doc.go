// Package sqlvocab is the shared shape a SQL vocabulary roster declares itself
// in: a function's name, its parameters in order, and — the part that is new —
// what each parameter *is* (ADR-0190 §SD4).
//
// Six packages in this repository publish a family of SQL function names as
// data, each with its own `Function` type around the same `Params []string`
// (ADR-0174 §SD3). Those strings are display spellings — `'Kind'`,
// `'section'`, `'chan:…'` — so a panel can render a call template. Nothing in
// them says that the first argument of `LW_COMPONENT` is a registered
// component kind, which is what an authoring surface needs to know before it
// can offer the kinds.
//
// So a [Param] carries a [Domain]: a kind, and for the kinds that need one,
// the ordinal of the sibling argument the answer depends on. The domain is
// declared beside the function, by whoever declares the function — one package
// away from the registry that resolves it, rather than in a central table that
// drifts (ADR-0190 Question 2, D1).
//
// # What a domain is not
//
// It is not a type, and it is not a validator. [DomainComponentKind] says
// "whatever `componentsql` has registered", and this package holds no opinion
// about what that is; resolving a domain to actual candidates is the host's
// job, wired per buffer through ADR-0147 §SD7's provider. A domain a host
// cannot resolve yields nothing, which is ADR-0190 §SD1's posture: silence,
// not a guess.
//
// # The registry
//
// [Registry] is the union of the rosters, populated at the host's wiring site
// beside the passes and the components — never by package init, for ADR-0189
// §SD7's reason: a registry filled by init has a link-set-dependent extent. The
// vocabulary panel and the completion engine read the same registry, so a
// roster cannot reach one surface and not the other.
//
// A name may be registered more than once when it genuinely belongs to more
// than one population — the `LW_ID_*` family is both a server UDF and a client
// macro — which is why [Registry.Lookup] answers with a slice. What is refused
// is the same name twice for overlapping populations.
package sqlvocab
