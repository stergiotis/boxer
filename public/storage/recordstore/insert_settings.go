package recordstore

import (
	"context"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
)

// ToggleE is a ClickHouse boolean setting a caller may leave unset.
type ToggleE uint8

const (
	ToggleUnset ToggleE = iota
	ToggleOn
	ToggleOff
)

// InsertSettings are the settings one insert carries (ADR-0309 §SD4 asks
// for async_insert = 0 on journal inserts). The zero value sets nothing.
type InsertSettings struct {
	AsyncInsert        ToggleE
	WaitForAsyncInsert ToggleE
	// DeduplicationToken, when set, is sent as insert_deduplication_token:
	// a table keeping a deduplication window drops a later insert carrying
	// a token it has seen, so a retry of an insert that landed is not
	// written twice (ADR-0387). A table without the window ignores it.
	DeduplicationToken string
}

// IsZero reports whether the settings set nothing.
func (inst InsertSettings) IsZero() bool {
	return inst.AsyncInsert == ToggleUnset && inst.WaitForAsyncInsert == ToggleUnset && inst.DeduplicationToken == ""
}

// WithDeduplicationToken is the settings with the token set.
func (inst InsertSettings) WithDeduplicationToken(token string) InsertSettings {
	inst.DeduplicationToken = token
	return inst
}

// Map is the settings as name → value, in a fixed order of names; nil when
// they set nothing.
func (inst InsertSettings) Map() (m map[string]string) {
	for _, s := range inst.pairs() {
		if m == nil {
			m = make(map[string]string, 3)
		}
		m[s[0]] = s[1]
	}
	return
}

// SQL is the settings as an INSERT's SETTINGS clause, with a leading
// space; empty when they set nothing.
func (inst InsertSettings) SQL() (clause string) {
	pairs := inst.pairs()
	if len(pairs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		if p[0] == "insert_deduplication_token" {
			parts = append(parts, p[0]+"="+quoteLiteral(p[1]))
			continue
		}
		parts = append(parts, p[0]+"="+p[1])
	}
	return " SETTINGS " + strings.Join(parts, ", ")
}

func (inst InsertSettings) pairs() (pairs [][2]string) {
	add := func(name string, t ToggleE) {
		switch t {
		case ToggleOn:
			pairs = append(pairs, [2]string{name, "1"})
		case ToggleOff:
			pairs = append(pairs, [2]string{name, "0"})
		}
	}
	add("async_insert", inst.AsyncInsert)
	add("wait_for_async_insert", inst.WaitForAsyncInsert)
	if inst.DeduplicationToken != "" {
		pairs = append(pairs, [2]string{"insert_deduplication_token", inst.DeduplicationToken})
	}
	return
}

// InsertSettingsI is an executor that carries settings per insert, beside
// [ExecutorI]'s InsertArrow. applied reports whether the settings reached
// the server: a wrapper over an executor without the seam inserts plainly
// and says so.
type InsertSettingsI interface {
	InsertArrowWith(ctx context.Context, table string, records []arrow.RecordBatch, settings InsertSettings) (applied bool, err error)
}

// InsertArrowWith inserts through exec with settings when exec carries
// them, and plainly otherwise; applied says which. Settings that set
// nothing are always applied.
func InsertArrowWith(ctx context.Context, exec ExecutorI, table string, records []arrow.RecordBatch, settings InsertSettings) (applied bool, err error) {
	if settings.IsZero() {
		return true, exec.InsertArrow(ctx, table, records)
	}
	if x, ok := exec.(InsertSettingsI); ok {
		return x.InsertArrowWith(ctx, table, records, settings)
	}
	return false, exec.InsertArrow(ctx, table, records)
}
