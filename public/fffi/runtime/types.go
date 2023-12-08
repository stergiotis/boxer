package runtime

// ForeignPtrInterface For documentation only (contains private methods)
type ForeignPtrInterface interface {
	getFffi() *Fffi2
	handleError(err error)
}
