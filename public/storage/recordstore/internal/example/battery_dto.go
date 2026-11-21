package example

// Battery is the charge-level device component; see identity_dto.go for
// the component-file layout rationale.
type Battery struct {
	_      struct{} `kind:"battery"`
	ID     uint64   `lw:",id"`
	Charge uint64   `lw:"deviceCharge,u64Array,unit"`
}
