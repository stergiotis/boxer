package example

// Tagged is the free-form label device component; see identity_dto.go for
// the component-file layout rationale.
type Tagged struct {
	_    struct{} `kind:"tagged"`
	ID   uint64   `lw:",id"`
	Tags []string `lw:"deviceTags,symbolArray"`
}
