package good

type WeekdayE uint8

const (
	WeekdayMonday WeekdayE = iota
	WeekdayTuesday
	WeekdayWednesday
)

type StatusE uint8

const (
	StatusDraft  StatusE = 1
	StatusStable StatusE = 2
)

const StatusExtra StatusE = 99 // single-spec extension, prefix still matches

type NotAnEnum int // only one value, not detected as enum

const SoloValue NotAnEnum = 1

type MissingESuffix uint8 // not E-suffixed — CS006 handles type name; CS007 skips

const (
	WrongFooA MissingESuffix = 1
	WrongFooB MissingESuffix = 2
)
