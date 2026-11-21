package coveragebus

import "github.com/stergiotis/boxer/public/keelson/runtime/app"

// The capabilities an app declares to use this service, one helper per
// capability taking the reason the manifest gives (ADR-0396 §6).

// PublishCap publishes coverage samples.
func PublishCap(reason string) (c app.SubjectFilter) {
	return app.SubjectFilter{Pattern: SubjectWildcard, Direction: app.CapDirectionPub, Reason: reason}
}

// SubscribeCap hears coverage samples.
func SubscribeCap(reason string) (c app.SubjectFilter) {
	return app.SubjectFilter{Pattern: SubjectWildcard, Direction: app.CapDirectionSub, Reason: reason}
}
