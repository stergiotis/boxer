package runtime

// ForeignPtrInterface is for documentation only — it contains private methods.
type ForeignPtrInterface interface {
	getFffi() *Fffi2
	handleError(err error)
}
