// Package chversion holds the oldest ClickHouse release boxer runs against
// and the checks that refuse an older one.
//
// The floor is checked where a round trip already happens: the facts store's
// setup reads version() beside the read surface's stamp, a store executor
// connecting through storeexec reads it after its ping, and the
// clickhouse-local pool asks the binary once per path before its first
// worker (extbin.Program.CheckVersion). A server or binary below the floor is
// refused with the floor and one feature that needs it, rather than failing
// later on a statement whose error does not say why.
//
// The check is a floor, not a pin: a newer release passes. A feature that a
// newer release changes is not detected here.
package chversion

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Floor is the oldest ClickHouse release boxer runs against, server and
// `clickhouse local` alike.
const Floor = "26.8"

// FloorFeature names one behaviour that needs the floor, for the refusal.
// Not the only one: lightweight UPDATE on by default (watchbill's claim),
// lambda-parameter SQL UDFs (the leeway read surface), query parameters
// inside EXPLAIN and refreshable materialized views are used as well.
const FloorFeature = "a statement's own FORMAT clause yields to the X-ClickHouse-Format header, which chserver relies on (ADR-0144)"

// SelectVersion is the expression a round trip adds to read the server's
// release.
const SelectVersion = "version()"

// Version is a ClickHouse release as version() reports it: year, month,
// patch and build. Missing trailing parts are zero.
type Version struct {
	Major int
	Minor int
	Patch int
	Build int
}

// Parse reads the first dotted-numeric token of s: a bare version() answer
// ("26.9.13.15"), or a banner around one ("ClickHouse local version
// 26.9.13.15 (official build)."). At least a major and a minor are needed.
func Parse(s string) (v Version, err error) {
	tok := firstDotted(s)
	if tok == "" {
		err = eb.Build().Str("text", s).Errorf("no ClickHouse version in the text")
		return
	}
	parts := strings.Split(tok, ".")
	var nums [4]int
	for i := 0; i < len(parts) && i < len(nums); i++ {
		nums[i], err = strconv.Atoi(parts[i])
		if err != nil {
			err = eb.Build().Str("text", s).Errorf("unreadable ClickHouse version: %w", err)
			return
		}
	}
	v = Version{Major: nums[0], Minor: nums[1], Patch: nums[2], Build: nums[3]}
	return
}

// firstDotted is the first run of digits and dots holding at least one dot
// between digits.
func firstDotted(s string) (tok string) {
	for i := 0; i < len(s); i++ {
		if !isDigit(s[i]) {
			continue
		}
		j := i
		for j < len(s) && (isDigit(s[j]) || s[j] == '.') {
			j++
		}
		cand := strings.Trim(s[i:j], ".")
		if strings.Contains(cand, ".") && !strings.Contains(cand, "..") {
			return cand
		}
		i = j
	}
	return ""
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

// Compare orders two versions: negative when inst is older than o, zero
// when equal, positive when newer.
func (inst Version) Compare(o Version) (c int) {
	for _, d := range [4]int{inst.Major - o.Major, inst.Minor - o.Minor, inst.Patch - o.Patch, inst.Build - o.Build} {
		if d != 0 {
			return d
		}
	}
	return 0
}

// String renders the version dotted, without trailing zero parts past the
// minor.
func (inst Version) String() (s string) {
	s = strconv.Itoa(inst.Major) + "." + strconv.Itoa(inst.Minor)
	if inst.Patch != 0 || inst.Build != 0 {
		s += "." + strconv.Itoa(inst.Patch)
	}
	if inst.Build != 0 {
		s += "." + strconv.Itoa(inst.Build)
	}
	return
}

// FloorVersion is Floor parsed.
func FloorVersion() (v Version) {
	v, _ = Parse(Floor)
	return
}

// Check refuses a reported release below the floor; what names where the
// release was read from ("server", "clickhouse local"), for the message.
func Check(what string, reported string) (err error) {
	v, err := Parse(reported)
	if err != nil {
		return
	}
	if v.Compare(FloorVersion()) < 0 {
		err = eb.Build().Str("source", what).Str("version", v.String()).Str("floor", Floor).
			Errorf("ClickHouse %s is below the floor boxer runs against (%s): %s", v.String(), Floor, FloorFeature)
	}
	return
}
