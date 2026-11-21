package clipboardbroker

import "github.com/stergiotis/boxer/public/keelson/runtime/app"

// The capabilities an app declares to use this service, one helper per
// capability taking the reason the manifest gives (ADR-0396 §6).

// WriteCap copies text to the clipboard.
func WriteCap(reason string) (c app.SubjectFilter) {
	return app.SubjectFilter{Pattern: SubjectWrite, Direction: app.CapDirectionPub, Reason: reason}
}

// ResolveCap reads what a clipboard handle names.
func ResolveCap(reason string) (c app.SubjectFilter) {
	return app.SubjectFilter{Pattern: SubjectResolve, Direction: app.CapDirectionPub, Reason: reason}
}
