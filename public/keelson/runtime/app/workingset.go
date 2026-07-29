package app

// workingset.go is the app-side half of ADR-0148: the optional compose
// hook a participating app implements, and the reason enum the host sets
// on the mount context so an app can tell a caller's config from its own
// restored one.

// WorkingsetComposerI is the one interface a workingset-participating app
// implements (ADR-0148 §SD4). The host calls ComposeWorkingset at window
// close (reap) and at shutdown, **before** Unmount — Unmount tears the app
// down, so composing after it would read a corpse — and writes the record
// iff dirty.
//
// cfg is an instance of the app's Manifest.LaunchKind DTO, encoded exactly
// as a caller would encode it for `windowhost.open` (§SD2): restore is
// definitionally OpenWithConfig, so there is no second serialization to
// maintain. The host validates the bytes against the manifest's kind and
// the launch-config size cap before storing them, and never stores bytes
// that fail.
//
// dirty means user intent occurred in this window — an edit, a manual
// Run, a deliberate view change. It is the whole save gate: a
// launch-seeded window closed untouched writes nothing, so an ephemeral
// seed cannot poison what the next plain open inherits. Byte-comparing
// against the stored record is deliberately not part of the gate (a
// composed config carrying a timestamp is never byte-equal anyway).
//
// A compose error is logged and skips the save; it never disturbs the
// close. Participation also requires factory registration and a
// Manifest declaring Workingset — see [Manifest.Workingset].
//
// Registration cannot check this: the manifest is known at Register time
// but the AppI instance only exists after the ctor runs at Open, so a
// manifest that declares Workingset over an app that does not implement
// this interface is diagnosed by the host at the first save attempt (one
// warning per app id), not at startup.
type WorkingsetComposerI interface {
	ComposeWorkingset() (cfg []byte, dirty bool, err error)
}

// LaunchReasonE says why the launch config on a MountContextI is there
// (ADR-0148 §SD5). Restore rides the same door as a caller-delivered
// config — the app decodes one thing in Mount either way — so without a
// reason an adopter could not keep its environment overrides between the
// two config tiers, which the documented precedence requires:
// caller config > env override > restored config > default.
//
// The gradation follows Wayland's session-management reasons
// (launch / recover / session_restore) minus the recover tier, which
// waits on the crash-recovery deferral.
type LaunchReasonE uint8

const (
	// LaunchReasonPlain: no config was delivered. The zero value, so a
	// context nobody set a reason on reads as a plain open.
	LaunchReasonPlain LaunchReasonE = 0
	// LaunchReasonCaller: another app asked for this window and supplied
	// the config (ADR-0135). Per-window intent — it outranks env.
	LaunchReasonCaller LaunchReasonE = 1
	// LaunchReasonRestore: the host recovered the config from a stored
	// workingset on an otherwise plain open. Ambient, not asked-for — so
	// an explicit env override outranks it.
	LaunchReasonRestore LaunchReasonE = 2
)

var AllLaunchReasons = []LaunchReasonE{
	LaunchReasonPlain,
	LaunchReasonCaller,
	LaunchReasonRestore,
}

func (inst LaunchReasonE) String() (s string) {
	switch inst {
	case LaunchReasonCaller:
		s = "caller"
	case LaunchReasonRestore:
		s = "restore"
	default:
		s = "plain"
	}
	return
}
