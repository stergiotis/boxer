package actor

// Actor is the descriptor fact and the dimension.Store's D: the claimed
// principal, purpose and app a surrogate id was minted for (ADR-0295 §SD8).
// ID is plain-bound to the envelope key: ignored on write (the store keys on
// Begin(id)), read back on Resolve.
type Actor struct {
	_         struct{} `kind:"actor"`
	ID        uint64   `lw:",id"`
	Principal string   `lw:"actorPrincipal,symbol"`
	Purpose   string   `lw:"actorPurpose,symbol"`
	App       string   `lw:"actorApp,symbol"`
}
