package adhocdata

import "github.com/stergiotis/boxer/public/keelson/runtime/app"

// The capabilities an app declares to use this service, one helper per
// capability taking the reason the manifest gives (ADR-0396 §6).

// PublishCap publishes an ad-hoc dataset.
func PublishCap(reason string) (c app.SubjectFilter) {
	return app.SubjectFilter{Pattern: SubjectPublish, Direction: app.CapDirectionPub, Reason: reason}
}

// RetractCap retracts a dataset the app published.
func RetractCap(reason string) (c app.SubjectFilter) {
	return app.SubjectFilter{Pattern: SubjectRetract, Direction: app.CapDirectionPub, Reason: reason}
}

// ResolveCap resolves a dataset by name.
func ResolveCap(reason string) (c app.SubjectFilter) {
	return app.SubjectFilter{Pattern: SubjectResolve, Direction: app.CapDirectionPub, Reason: reason}
}

// ReadCap reads a dataset whole.
func ReadCap(reason string) (c app.SubjectFilter) {
	return app.SubjectFilter{Pattern: SubjectRead, Direction: app.CapDirectionPub, Reason: reason}
}

// BundlePublishCap publishes a bundle of datasets.
func BundlePublishCap(reason string) (c app.SubjectFilter) {
	return app.SubjectFilter{Pattern: SubjectBundlePublish, Direction: app.CapDirectionPub, Reason: reason}
}

// BundleRetractCap retracts a bundle the app published.
func BundleRetractCap(reason string) (c app.SubjectFilter) {
	return app.SubjectFilter{Pattern: SubjectBundleRetract, Direction: app.CapDirectionPub, Reason: reason}
}

// BundleResolveCap resolves a bundle by name.
func BundleResolveCap(reason string) (c app.SubjectFilter) {
	return app.SubjectFilter{Pattern: SubjectBundleResolve, Direction: app.CapDirectionPub, Reason: reason}
}

// EventsCap hears datasets published and retracted.
func EventsCap(reason string) (c app.SubjectFilter) {
	return app.SubjectFilter{Pattern: SubjectEventAll, Direction: app.CapDirectionSub, Reason: reason}
}

// BundleEventsCap hears bundles published and retracted.
func BundleEventsCap(reason string) (c app.SubjectFilter) {
	return app.SubjectFilter{Pattern: SubjectBundleEventAll, Direction: app.CapDirectionSub, Reason: reason}
}
