package appletstore

import "github.com/stergiotis/boxer/public/keelson/runtime/app"

// The capabilities an app declares to use this service, one helper per
// capability taking the reason the manifest gives (ADR-0396 §6).

// SaveCap saves an applet document.
func SaveCap(reason string) (c app.SubjectFilter) {
	return app.SubjectFilter{Pattern: SubjectSave, Direction: app.CapDirectionBoth, Reason: reason}
}
