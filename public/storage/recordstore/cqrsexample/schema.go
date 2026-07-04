// Package cqrsexample is the ADR-0100 slice-S4 worked example: a minimal
// event-sourced account ledger (CQRS write model) built on a generated
// recordstore, documentation-grade. It demonstrates the ADR's
// event-sourcing mapping end to end:
//
//   - aggregate id   = envelope Key ("acct/<n>"; snapshots live under the
//     sibling key "acct/<n>/snap" — the namespace idiom from
//     recordstore/pushoutstore)
//   - event sequence = envelope Order, caller-assigned synthetic nanos
//     (single-writer per aggregate; optimistic concurrency is deferred,
//     see ADR-0100)
//   - event type     = archetype (which component the row carries)
//   - event payload  = components (Opened / Deposited / Withdrawn /
//     Closed, one section each)
//   - append         = Begin(id, seq).Add*(…).Commit() + Flush
//   - rehydrate      = Latest(snapshot key) + Replay(id, after snapshot)
//     folded into state (account.go)
//
// The ledger deliberately has NO lifecycle column: events are never
// deleted, so the store generator emits no state view — closing an
// account is a domain event (Closed), not a storage tombstone. The
// cached Get path is likewise unused here: it serves immutable-by-key
// consumers (content-addressed envelopes); an aggregate rehydrates
// through Replay.
package cqrsexample

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	easp "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// TableRowConfig matches anchor's: multiple attributes per row.
const TableRowConfig = common.TableRowConfigMultiAttributesPerRow

// GetLedgerSchemaInManipulator builds the ledger fact table. Envelope
// roles: id (string Key), ts (Order — the caller-assigned event
// sequence). Every component owns distinct sections so the per-kind
// membership ids cannot collide in storage.
func GetLedgerSchemaInManipulator() (manip *common.TableManipulator, err error) {
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("create table manipulator: %w", err)
		return
	}
	manip.SetTableName("ledger")
	manip.SetTableComment("event-sourced account ledger (ADR-0100 S4)")
	loadLedgerSchema(manip)
	return
}

func loadLedgerSchema(manip common.TableManipulatorFluidI) {
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.S).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression)
	manip.PlainValueColumn(common.PlainItemTypeEntityTimestamp, "ts", ctabb.Z64).
		AddColumnEncodingHints(easp.AspectDeltaEncoding, easp.AspectLightGeneralCompression)

	channels := []common.MembershipSpecE{
		common.MembershipSpecLowCardRef,
		common.MembershipSpecLowCardVerbatim,
		common.MembershipSpecHighCardRef,
		common.MembershipSpecMixedLowCardRefHighCardParameters,
	}
	section := func(name naming.StylableName, ct canonicaltypes.PrimitiveAstNodeI) {
		sec := manip.TaggedValueSection(name).
			SectionStreamingGroup("data").
			AddSectionMembership(channels...)
		sec.TaggedValueColumn("value", ct).
			AddColumnEncodingHints(easp.AspectLightGeneralCompression)
	}

	u64Arr := canonicaltypes.PromoteScalarPrim(ctabb.U64, canonicaltypes.ScalarModifierHomogenousArray)
	// Event-payload sections.
	section("acctOwner", ctabb.S)   // Opened.Owner
	section("acctDeposit", u64Arr)  // Deposited.Amount (unit)
	section("acctWithdraw", u64Arr) // Withdrawn.Amount (unit)
	section("acctClosed", ctabb.S)  // Closed.Reason
	// Snapshot sections (the folded state, written under the sibling key).
	section("snapOwner", ctabb.S)  // AccountState.Owner
	section("snapBalance", u64Arr) // AccountState.Balance (unit)
	section("snapClosed", ctabb.B) // AccountState.Closed (bool scalar)
	section("snapAsOf", u64Arr)    // AccountState.AsOf (unit; covered sequence)
}
