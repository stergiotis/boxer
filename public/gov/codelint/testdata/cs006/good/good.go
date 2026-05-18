package good

type StatusE uint8

const (
	StatusDraft  StatusE = 1
	StatusStable StatusE = 2
)

type WeekdayE uint8

const (
	WeekdayMonday WeekdayE = iota
	WeekdayTuesday
	WeekdayWednesday
)

type Lonely int // not detected as enum — only one value

const SingleLonely Lonely = 1

const ( // mixed: untyped consts, no named type involved
	A = 1
	B = 2
)
