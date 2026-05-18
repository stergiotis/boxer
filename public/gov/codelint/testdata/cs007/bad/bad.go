package bad

type ColorE uint8

const (
	Red   ColorE = iota // want CS007 here
	Green               // want CS007 here
	Blue                // want CS007 here
)

type LevelE uint8

const (
	LevelInfo  LevelE = 1
	WarnLevel  LevelE = 2 // want CS007 here
	LevelError LevelE = 3
)

const ExtraColor ColorE = 99 // want CS007 here — single-spec extension also flagged

type SuppressedE uint8

const (
	NotPrefixedA SuppressedE = 1 //boxer:lint disable=CS007 reason="testdata coverage of suppression"
	NotPrefixedB SuppressedE = 2 // want CS007 here
)
