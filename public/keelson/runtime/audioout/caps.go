package audioout

import "github.com/stergiotis/boxer/public/keelson/runtime/app"

// The capabilities an app declares to use this service, one helper per
// capability taking the reason the manifest gives (ADR-0396 §6).

// OpenCap asks the host for an audio output.
func OpenCap(reason string) (c app.SubjectFilter) {
	return app.SubjectFilter{Pattern: SubjectOpen, Direction: app.CapDirectionPub, Reason: reason}
}
