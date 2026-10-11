package agentrunstore

import (
	"slices"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
)

// TableNameBody is the vault table's bare name (ADR-0320 §SD1).
const TableNameBody = "agentrunbody"

// GetBodySchemaInManipulator builds the vault table: the journal envelope
// (the run id and the entry's ordinal) over the boxer.facts data
// sections, which here also take the verbatim name + parameters channel a
// shredded document's paths ride on, and the three value-less sections
// of the shredder's lossless mode (plan002 B0, B3). One row per entry that
// carries content; rows are destroyed, never rewritten, so the table has
// no lifecycle column and takes the heavyweight mutation the ledger never
// sees.
func GetBodySchemaInManipulator() (manip *common.TableManipulator, err error) {
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("create table manipulator: %w", err)
		return
	}
	manip.SetTableName(TableNameBody)
	manip.SetTableComment("agent run journal bodies, one row per entry with content, destroyed on erasure (ADR-0320 §SD1)")
	journalEnvelope(manip)
	factsschema.LoadFactsDataSections(manip, common.MembershipSpecMixedLowCardVerbatimHighCardParameters)
	factsschema.LoadValuelessSections(manip, append(slices.Clone(factsschema.FactsMembershipSpecs), common.MembershipSpecMixedLowCardVerbatimHighCardParameters)...)
	return
}
