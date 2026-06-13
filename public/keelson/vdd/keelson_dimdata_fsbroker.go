package vdd

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/stopa/registry"
)

// Fsbroker narrow memberships — the request/reply/event DTOs for
// fs.dialog.* and fs.handle.{uuid}.* subjects. Shared columns
// (reason) live in keelson_dimdata_shared.go.
//
// dialog- vs watch- prefixes split the broker's two surfaces. The
// "Approved" naming is intentionally kept narrow (`dialogApproved`)
// rather than reusing capbroker's `grantApproved` — both are
// broker-says-yes booleans but the domains differ (file-picker UI
// vs capability policy). If a third "approval" DTO emerges, that's
// the trigger to elevate to a shared `approved` term.
var (
	// MembDialogApproved is true when the user accepted the file
	// picker, false on cancel or broker-side error.
	MembDialogApproved = KeelsonHrNkRegistry.MustBegin("dialogApproved").
				MustAddRestriction("bool", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()

	// MembDialogHandleSubject is the NATS subject prefix
	// (fs.handle.{uuid}) the broker hands back on approval — the
	// caller's caps already cover {prefix}.> by the time the reply
	// lands.
	MembDialogHandleSubject = KeelsonHrNkRegistry.MustBegin("dialogHandleSubject").
				MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()

	// MembWatchPollFallback forces the poller backend regardless of
	// the underlying filesystem (mirrors WatchRequest.PollFallback).
	MembWatchPollFallback = KeelsonHrNkRegistry.MustBegin("watchPollFallback").
				MustAddRestriction("bool", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()

	// MembWatchPollIntervalMs is the poller tick interval (zero
	// selects default 500ms; values below 100ms clamp).
	MembWatchPollIntervalMs = KeelsonHrNkRegistry.MustBegin("watchPollIntervalMs").
				MustAddRestriction("i32Array", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()

	// MembWatchRecursive enables subtree watching from the handle's
	// root path.
	MembWatchRecursive = KeelsonHrNkRegistry.MustBegin("watchRecursive").
				MustAddRestriction("bool", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()

	// MembWatchStarted is true when the watch actually started; false
	// on broker-side error (with `reason` populated) or when the
	// handle already has an active watch.
	MembWatchStarted = KeelsonHrNkRegistry.MustBegin("watchStarted").
				MustAddRestriction("bool", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()

	// MembWatchEventSubject is the NATS subject events publish to
	// (fs.handle.{uuid}.event).
	MembWatchEventSubject = KeelsonHrNkRegistry.MustBegin("watchEventSubject").
				MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()

	// MembWatchBackend names the watcher implementation selected at
	// pickBackend ("inotify", "poller"). Symbol section because the
	// enumeration is small + stable.
	MembWatchBackend = KeelsonHrNkRegistry.MustBegin("watchBackend").
				MustAddRestriction("symbol", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()

	// MembWatchEventKind is fsbroker.WatchEventKindE rendered as its
	// canonical String() ("create" / "delete" / "modify" / "attrib"
	// / "renameFrom" / "renameTo" / "overflow" / "closed" /
	// "unspecified"). Symbol for the LowCardinality dictionary.
	MembWatchEventKind = KeelsonHrNkRegistry.MustBegin("watchEventKind").
				MustAddRestriction("symbol", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()

	// MembWatchEventName is the basename (single-level mode) or
	// forward-slash relative path (recursive mode) of the affected
	// entry. Empty when the event addresses the watched root.
	MembWatchEventName = KeelsonHrNkRegistry.MustBegin("watchEventName").
				MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()

	// MembWatchEventCookie pairs inotify RenameFrom/RenameTo events;
	// zero on poller-backed watches.
	MembWatchEventCookie = KeelsonHrNkRegistry.MustBegin("watchEventCookie").
				MustAddRestriction("u32Array", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
)
