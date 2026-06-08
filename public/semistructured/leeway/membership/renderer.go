package membership

// Renderer turns a MembershipValue's wire identity into display strings via
// injectable formatters. Representation is the Renderer's output — producers
// emit identities, consumers render them (ADR-0072). Construct via NewRenderer
// or DefaultRenderer; the zero value is not usable.
//
// The injectable-formatter seam (RefFormatterI / VerbatimFormatterI /
// ParamsFormatterI) is deliberately retained even though every current consumer
// uses DefaultRenderer: it is ADR-0072's representation plane, and the first
// real injector — a registry-backed RefFormatter rendering low-card refs as
// their human names, the inverse of the write-side name→id lookup registry — is
// deferred, not abandoned (decided 2026-06-08). Do not collapse the seam to
// package functions on the assumption it is dead code.
type Renderer struct {
	ref      RefFormatterI
	verbatim VerbatimFormatterI
	params   ParamsFormatterI
}

// NewRenderer builds a Renderer from the three membership formatters; a nil
// formatter falls back to its default (hex ref / bytes-as-string).
func NewRenderer(ref RefFormatterI, verbatim VerbatimFormatterI, params ParamsFormatterI) *Renderer {
	if ref == nil {
		ref = DefaultRefFormatter{}
	}
	if verbatim == nil {
		verbatim = DefaultVerbatimFormatter{}
	}
	if params == nil {
		params = DefaultParamsFormatter{}
	}
	return &Renderer{ref: ref, verbatim: verbatim, params: params}
}

// DefaultRenderer is a Renderer with the default hex/bytes formatters.
func DefaultRenderer() *Renderer {
	return NewRenderer(nil, nil, nil)
}

// RenderRef renders a ref id (e.g. "0x2a" with the default formatter).
func (r *Renderer) RenderRef(ref uint64) string { return r.ref.FormatRef(ref) }

// RenderVerbatim renders a verbatim membership name.
func (r *Renderer) RenderVerbatim(verbatim string) string {
	return r.verbatim.FormatVerbatim([]byte(verbatim))
}

// RenderParams renders a membership params blob.
func (r *Renderer) RenderParams(params string) string {
	return r.params.FormatParams([]byte(params))
}

// Render renders mv's primary display string by Kind (ref kinds → RenderRef,
// verbatim kinds → RenderVerbatim). Params are not appended; callers that key
// on params compose RenderParams themselves.
func (r *Renderer) Render(mv MembershipValue) string {
	switch mv.Kind {
	case MembershipKindRef, MembershipKindRefParametrized, MembershipKindMixedLowCardRefHighCardParam:
		return r.RenderRef(mv.Ref)
	case MembershipKindVerbatim, MembershipKindMixedLowCardVerbatimHighCardParam:
		return r.RenderVerbatim(mv.Verbatim)
	}
	return ""
}
