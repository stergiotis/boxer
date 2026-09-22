package persiststore

// Owner is carried by every live row of every kind: which app the state
// belongs to, and which process and window wrote this version of it.
//
// It is a component of its own rather than fields on each kind because a
// generated store refuses two kinds naming one membership, and all three
// kinds need the same three: `runtimeApp`, `runtimeRun` and
// `runtimeLifecycleTileKey`. Split out, a row's archetype reads as what it
// is — Owner plus exactly one kind — and "everything this app owns" is one
// scan over one component, whichever kind the rows are.
//
// AppId repeats the app segment of the key in plain form, so SQL filters on
// a column rather than parsing an escaped key. RunId and InstanceKey are
// provenance, not identity (ADR-0191 §SD5): a second window writing the
// same key overwrites rather than forks, and now records that it did. A
// tombstone carries no Owner — the generated Delete writes no component —
// so a deletion is attributed by its key alone.
type Owner struct {
	_           struct{} `kind:"stateOwner"`
	ID          string   `lw:",id"`
	AppId       string   `lw:"runtimeApp,symbol"`
	RunId       string   `lw:"runtimeRun,symbol"`
	InstanceKey uint64   `lw:"runtimeLifecycleTileKey,u64"`
}
