package fsbroker

import "github.com/stergiotis/boxer/public/keelson/runtime/app"

// The capabilities an app declares to use this service, one helper per
// capability taking the reason the manifest gives (ADR-0396 §6).

// DialogReadCap asks the person to pick a file to read.
func DialogReadCap(reason string) (c app.SubjectFilter) {
	return app.SubjectFilter{Pattern: SubjectDialogRead, Direction: app.CapDirectionPub, Reason: reason}
}

// DialogWriteCap asks the person to pick where to write a file.
func DialogWriteCap(reason string) (c app.SubjectFilter) {
	return app.SubjectFilter{Pattern: SubjectDialogWrite, Direction: app.CapDirectionPub, Reason: reason}
}

// DialogBundleCap asks the person to pick a bundle of files.
func DialogBundleCap(reason string) (c app.SubjectFilter) {
	return app.SubjectFilter{Pattern: SubjectDialogBundle, Direction: app.CapDirectionPub, Reason: reason}
}

// DialogWatchCap asks the person to pick a folder to watch.
func DialogWatchCap(reason string) (c app.SubjectFilter) {
	return app.SubjectFilter{Pattern: SubjectDialogWatch, Direction: app.CapDirectionPub, Reason: reason}
}

// AppDataCap reaches the app's own data directory.
func AppDataCap(reason string) (c app.SubjectFilter) {
	return app.SubjectFilter{Pattern: SubjectAppDataPrefix + ">", Direction: app.CapDirectionPub, Reason: reason}
}
