package sessiondata

import "github.com/stergiotis/boxer/public/keelson/runtime/app"

// The capabilities an app declares to use this service, one helper per
// capability taking the reason the manifest gives (ADR-0396 §6).

// OpenCap opens the person's data session for a window.
func OpenCap(reason string) (c app.SubjectFilter) {
	return app.SubjectFilter{Pattern: SubjectOpen, Direction: app.CapDirectionPub, Reason: reason}
}

// AttachCap attaches a window to a data session by its ticket.
func AttachCap(reason string) (c app.SubjectFilter) {
	return app.SubjectFilter{Pattern: SubjectAttach, Direction: app.CapDirectionPub, Reason: reason}
}

// CloseCap ends a person's data session the window holds.
func CloseCap(reason string) (c app.SubjectFilter) {
	return app.SubjectFilter{Pattern: SubjectClose, Direction: app.CapDirectionPub, Reason: reason}
}

// DefineCap defines and redefines data sets.
func DefineCap(reason string) (c app.SubjectFilter) {
	return app.SubjectFilter{Pattern: SubjectDefine, Direction: app.CapDirectionPub, Reason: reason}
}

// DropCap drops data sets.
func DropCap(reason string) (c app.SubjectFilter) {
	return app.SubjectFilter{Pattern: SubjectDrop, Direction: app.CapDirectionPub, Reason: reason}
}

// PrepareCap readies statements to run in a data session.
func PrepareCap(reason string) (c app.SubjectFilter) {
	return app.SubjectFilter{Pattern: SubjectPrepare, Direction: app.CapDirectionPub, Reason: reason}
}

// ReleaseCap ends the hold a prepared statement has on its server session.
func ReleaseCap(reason string) (c app.SubjectFilter) {
	return app.SubjectFilter{Pattern: SubjectRelease, Direction: app.CapDirectionPub, Reason: reason}
}

// ListCap lists a data session's data sets.
func ListCap(reason string) (c app.SubjectFilter) {
	return app.SubjectFilter{Pattern: SubjectList, Direction: app.CapDirectionPub, Reason: reason}
}

// HandOverCap mints hand-over tokens for a person's data session a window
// holds, to hand it to a window it opens.
func HandOverCap(reason string) (c app.SubjectFilter) {
	return app.SubjectFilter{Pattern: SubjectHandOver, Direction: app.CapDirectionPub, Reason: reason}
}

// ClaimCap attaches a window by a hand-over token.
func ClaimCap(reason string) (c app.SubjectFilter) {
	return app.SubjectFilter{Pattern: SubjectClaim, Direction: app.CapDirectionPub, Reason: reason}
}
