// Package rowcas composes the statements of a compare-and-set on one row of
// a ClickHouse MergeTree table, and carries the conditions under which that
// is what they are.
//
// The primitive (ADR-0223 §SD3): a conditional lightweight UPDATE that
// guards on the row's current values and writes a mark, followed by a read
// of the row by the same client — the read-back is the answer, and the
// caller holds what it asked for exactly when the row says so. Under the
// conditions below it is a linearizable compare-and-set on that row; what
// it is not — a transaction over two rows, a durable write under power
// loss, a claim that survives a replicated table — is written down in
// doc/explanation/watchbill-consistency-model.md, in Jepsen's terms.
//
// The conditions, each of which this package either states on the
// statement or has no way to enforce and says so:
//
//   - every update carries update_parallel_mode='sync' ([UpdateSettings]),
//     so the server serialises them rather than trusting its dependency
//     analysis — stated by [UpdateSQL];
//   - the table carries the block-position columns a lightweight update
//     needs ([TableSettingsTail], [AlterTableSettingsSQL]);
//   - no heavyweight mutation ever runs on the table: a DELETE is a
//     lightweight update ([DeleteSQL]), and the only ALTER is the settings
//     one. A mutation rewriting a part under a patch was the one condition
//     measured to let two claims both win. Nothing here can stop an
//     operator's ALTER; the rule is enforced by every writer composing
//     through this package and nothing else;
//   - the row's key is immutable, since a lightweight update cannot touch a
//     key column;
//   - async_insert never sits on the claim path;
//   - one server: the serialising property is one server's.
//
// A table that meets them is one a second coordination primitive can be
// built on with the same model; one that does not is not.
package rowcas

import "strings"

// UpdateSettings is what every update carries: the server's sequential
// mode, stated rather than left to the default's dependency analysis.
const UpdateSettings = " SETTINGS update_parallel_mode='sync'"

// DeleteSettings makes a DELETE a patch-part delete rather than the default
// heavyweight mutation, so it serialises with the updates.
const DeleteSettings = " SETTINGS lightweight_delete_mode='lightweight_update'"

// TableSettingsTail is what a table's CREATE gains beyond its generator's
// own settings: the two block-position columns the lightweight UPDATE needs.
// It extends a SETTINGS clause the DDL ends with.
const TableSettingsTail = ", enable_block_number_column=1, enable_block_offset_column=1"

// AlterTableSettingsSQL puts the same pair on a table that exists already;
// MODIFY SETTING is idempotent and is the one ALTER a guarded table admits.
func AlterTableSettingsSQL(table string) (sql string) {
	return "ALTER TABLE " + table + " MODIFY SETTING enable_block_number_column=1, enable_block_offset_column=1"
}

// Set is one column assignment of an update; Expr is SQL, already rendered.
type Set struct {
	Column string
	Expr   string
}

// UpdateSQL renders the guarded update: sets, then the where the caller
// composed — the key equality and the guards on the current values — then
// the settings. An empty where is refused by rendering nothing, since an
// unguarded update of a whole table is not a compare-and-set.
func UpdateSQL(table string, sets []Set, where string) (sql string) {
	if len(sets) == 0 || strings.TrimSpace(where) == "" {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("UPDATE " + table + " SET ")
	for i, s := range sets {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(s.Column + " = " + s.Expr)
	}
	sb.WriteString(" WHERE " + where + UpdateSettings)
	return sb.String()
}

// DeleteSQL renders the lightweight delete of the rows where holds.
func DeleteSQL(table string, where string) (sql string) {
	if strings.TrimSpace(where) == "" {
		return ""
	}
	return "DELETE FROM " + table + " WHERE " + where + DeleteSettings
}
