package functional

// TranslateEmpty if s is the empty value (type specific) TranslateEmpty returns replacement
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
