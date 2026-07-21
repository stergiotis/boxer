package vdd

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/stopa/registry"
)

// Windowhost narrow memberships — the launch request/reply DTOs
// (LaunchRequest, LaunchReply) for the audited `windowhost.open` subject
// (ADR-0135). Shared columns live in keelson_dimdata_shared.go: appId
// (the launch target), tileKey (the opened window's key in the reply —
// the same identifier the app-lifecycle rows carry), reason (the reply's
// error text, empty on success).
//
// Registration-order note: membership ids are assigned sequentially at
// init, and package files initialise in lexical file-name order. This
// file deliberately sorts after every other keelson_dimdata_*.go file so
// its entries append to the id space instead of renumbering existing
// entries (which on-disk facts data depends on). Future launch-surface
// entries (adopting apps' config columns) belong at the end of this
// file; a new dimdata file must sort after this one.
var (
	// MembLaunchConfigKind is the vocabulary kind name the launch
	// config bytes claim (e.g. "playLaunch"). Symbol section: the set
	// of launch-config kinds is small and closed by construction
	// (ADR-0135 §SD2 — a kind absent from the vocabulary has no codec).
	MembLaunchConfigKind = KeelsonHrNkRegistry.MustBegin("launchConfigKind").
				MustAddRestriction("symbol", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()

	// MembLaunchConfig is the launch config payload: the config DTO's
	// own facts-CBOR bytes, opaque at this level. Empty means "open
	// plainly". The host caps the size (64 KiB) at the boundary before
	// any decode.
	MembLaunchConfig = KeelsonHrNkRegistry.MustBegin("launchConfig").
				MustAddRestriction("blobArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
)

// PlayLaunch config columns (ADR-0135 §SD7) — the launch config play
// accepts (apps/play/launchcfg, kind playLaunch). Text sections for the
// two SQL columns (open-cardinality), symbol for the tab id (small
// closed set), bool for the two flags.
var (
	// MembPlayLaunchSql is the initial editor buffer.
	MembPlayLaunchSql = KeelsonHrNkRegistry.MustBegin("playLaunchSql").
				MustAddRestriction("textArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()

	// MembPlayLaunchAutoRun triggers a Run of the seeded buffer on mount.
	MembPlayLaunchAutoRun = KeelsonHrNkRegistry.MustBegin("playLaunchAutoRun").
				MustAddRestriction("bool", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()

	// MembPlayLaunchLive enables live re-run on the main lane.
	MembPlayLaunchLive = KeelsonHrNkRegistry.MustBegin("playLaunchLive").
				MustAddRestriction("bool", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()

	// MembPlayLaunchBandsSql seeds the Timeline panel's bands editor.
	// Empty means "leave the persisted/default bands untouched".
	MembPlayLaunchBandsSql = KeelsonHrNkRegistry.MustBegin("playLaunchBandsSql").
				MustAddRestriction("textArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()

	// MembPlayLaunchTab selects the initially focused body tab by id.
	// Empty means the default tab.
	MembPlayLaunchTab = KeelsonHrNkRegistry.MustBegin("playLaunchTab").
				MustAddRestriction("symbol", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
)
