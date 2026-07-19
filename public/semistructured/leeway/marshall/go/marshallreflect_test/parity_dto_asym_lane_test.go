package marshallreflect_test

import lw "github.com/stergiotis/boxer/public/semistructured/leeway/marshall/lw"

// parityAsymLane: an entity-level lw lane marker — a DOCUMENTED front-end
// asymmetry. Reflect relabels the field's canonical ("v") from the marker
// type; codegen has no value-marker bridge (deferred surface, ADR-0113 D3)
// and rejects the unknown Go type.
type parityAsymLane struct {
	_    struct{} `kind:"parityAsymLane"`
	ID   uint64   `lw:",id"`
	Addr lw.IPv4  `lw:"addr,ipv4Section"`
}
