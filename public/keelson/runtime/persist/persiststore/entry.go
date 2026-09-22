package persiststore

import (
	"errors"
	"strings"
)

// The kinds of state the table holds, as the manager names them (ADR-0185
// §SD1): keelson('app_state') shows them and runtime.appstate.delete takes
// them back. KindUnknown is a live row carrying none of the kinds this build
// knows — a kind added to the store before its readers learn it, which is
// still stored state and must stay visible and clearable.
const (
	KindPersist     = "persist"
	KindWorkingset  = "workingset"
	KindColumnWidth = "column_width"
	KindUnknown     = "unknown"
)

// KindOf names the kind of state ent holds.
func KindOf(ent *PersistEntity) string {
	switch {
	case ent.State.Has:
		return KindPersist
	case ent.Workingset.Has:
		return KindWorkingset
	case ent.ColumnWidth.Has:
		return KindColumnWidth
	}
	return KindUnknown
}

// EntryKeyOf is ent's identity within its app and kind — the persist key,
// the workingset name, or `tier/scope/column_key` — and, for a row of an
// unknown kind, the store key itself, the only name such a row has.
func EntryKeyOf(ent *PersistEntity) string {
	switch {
	case ent.State.Has:
		return ent.State.Val.Key
	case ent.Workingset.Has:
		return ent.Workingset.Val.Name
	case ent.ColumnWidth.Has:
		v := ent.ColumnWidth.Val
		return v.Tier + "/" + v.Scope + "/" + v.ColumnKey
	}
	return ent.ID
}

// ErrUnaddressableKind is what EntityIdOf answers for a kind it cannot turn
// into a store key: KindUnknown, whose only name is the store key, or a
// label no kind carries.
var ErrUnaddressableKind = errors.New("persiststore: the kind has no (app, key) address; name the entity by its store key")

// EntityIdOf is the store key of the entry (kind, appId, key) names — the
// inverse of EntryKeyOf over the known kinds. A column width's key is
// `tier/scope/column_key` where the scope may itself contain "/": the tier
// is the first segment, the column key the last, and the scope what lies
// between.
func EntityIdOf(kind string, appId string, key string) (id string, err error) {
	switch kind {
	case KindPersist:
		return StateKey(appId, key), nil
	case KindWorkingset:
		return WorkingsetKey(appId, key), nil
	case KindColumnWidth:
		first, last := strings.IndexByte(key, '/'), strings.LastIndexByte(key, '/')
		if first < 0 || first == last {
			return "", errors.New("persiststore: a column-width key is tier/scope/column_key")
		}
		return ColumnWidthKey(appId, key[:first], key[first+1:last], key[last+1:]), nil
	}
	return "", ErrUnaddressableKind
}
