package functional

// TranslateEmpty returns replacement when s is the empty value (type-specific), otherwise s.
func TranslateEmpty[T comparable](s T, replacement T) (r T) {
	if s == r {
		return replacement
	}
	return s
}

type InterfaceIsReferentialTransparentType bool

type PromiseReferentialTransparentI interface {
	PromiseToBeReferentialTransparent() (_ InterfaceIsReferentialTransparentType)
}