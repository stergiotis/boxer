package factsschema

import (
	"regexp"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// QualifiedTableName is the facts table as every writer in the tree spells
// it: the reference an insert names, and the one an insert transform is
// registered for (ADR-0402 §SD1).
const QualifiedTableName = DatabaseName + "." + TableName

// ReadTableSetting names the relation every read of facts goes to (ADR-0402
// §SD3). Empty reads [QualifiedTableName].
var ReadTableSetting = env.NewString(env.Spec{
	Name: "BOXER_FACTS_READ_TABLE",
	Description: "the relation every read of facts goes to, database-qualified: a view over a deployment's own table, or a Merge over storage tables. " +
		"It must return every boxer.facts column under its name and with its type; nothing about its engine or layout is checked. Empty reads boxer.facts",
	Category: env.CategoryDatabase,
})

// qualifiedRefRe is a database-qualified reference of the shape
// recordstore.CheckTableRef accepts, unquoted identifiers. It is repeated
// here so that the schema package does not depend on the record-store
// runtime. The qualification is required: the readers resolve the facts
// columns against the relation's database, and a bare name would resolve in
// whatever database a session starts in.
var qualifiedRefRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*\.[A-Za-z_][A-Za-z0-9_]*$`)

// ReadTable is the relation reads of facts go to: [ReadTableSetting], or
// [QualifiedTableName] when it is empty. A setting that is not a
// database-qualified, unquoted identifier is an error rather than a
// reference spliced into SQL. The registry resolves the setting once per
// process.
func ReadTable() (ref string, err error) {
	ref = ReadTableSetting.Get()
	if ref == "" {
		return QualifiedTableName, nil
	}
	if !qualifiedRefRe.MatchString(ref) {
		err = eb.Build().Str("setting", ReadTableSetting.Spec().Name).Str("value", ref).
			Errorf("factsschema: BOXER_FACTS_READ_TABLE must be a database-qualified, unquoted identifier")
		ref = ""
	}
	return
}

// ReadTableFor is what a store bound to table is given as its read table: the
// configured relation when table is [QualifiedTableName] and the setting
// names another one, and "" otherwise, which the stores take as "read the
// table written to". A store over another table — a scratch table, a
// snapshot store's own — is unaffected.
func ReadTableFor(table string) (ref string, err error) {
	if table != QualifiedTableName {
		return "", nil
	}
	ref, err = ReadTable()
	if err != nil || ref == QualifiedTableName {
		ref = ""
	}
	return
}
