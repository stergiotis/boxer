package callsites

// Summary aggregates a survey run for the ADR-0107 §SD7 completion line and
// for programmatic consumers that want counts rather than the site stream.
type Summary struct {
	Total uint64

	Unknown     uint64
	Mono        uint64
	StaticPoly  uint64
	DynPoly     uint64
	Conversions uint64
	Builtins    uint64

	// Type-argument shape counts across TypeArgs and RecvTypeArgs.
	StenciledArgs uint64
	PointerArgs   uint64
	InterfaceArgs uint64
	TypeParamArgs uint64

	// Adjudication counts (ADR-0107 §SD1).
	Checked       uint64
	Devirtualized uint64
	InlinedCalls  uint64
}

func (inst *Summary) Add(site CallSite) {
	inst.Total++
	switch site.Type {
	case CallTypeMonomorphic:
		inst.Mono++
	case CallTypeStaticPolymorphic:
		inst.StaticPoly++
	case CallTypeDynamicPolymorphic:
		inst.DynPoly++
	case CallTypeConversion:
		inst.Conversions++
	case CallTypeBuiltin:
		inst.Builtins++
	default:
		inst.Unknown++
	}
	for _, args := range [2][]TypeArgInfo{site.TypeArgs, site.RecvTypeArgs} {
		for _, arg := range args {
			switch arg.Shape {
			case ShapeClassStenciled:
				inst.StenciledArgs++
			case ShapeClassPointer:
				inst.PointerArgs++
			case ShapeClassInterface:
				inst.InterfaceArgs++
			case ShapeClassTypeParam:
				inst.TypeParamArgs++
			}
		}
	}
	if site.Compiler.Checked {
		inst.Checked++
		if site.Compiler.Devirtualized {
			inst.Devirtualized++
		}
		if site.Compiler.InlinedCall {
			inst.InlinedCalls++
		}
	}
}
