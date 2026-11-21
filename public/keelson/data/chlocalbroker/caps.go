package chlocalbroker

import "github.com/stergiotis/boxer/public/keelson/runtime/app"

// The capabilities an app declares to use the broker, one helper per
// capability taking the reason the manifest gives (ADR-0396 §6).

// ExecCap runs statements on the pool named pool.
func ExecCap(pool string, reason string) (c app.SubjectFilter) {
	return app.SubjectFilter{Pattern: SubjectExecPrefix + pool, Direction: app.CapDirectionPub, Reason: reason}
}

// ExecAllCap runs statements on any pool, and hears the replies.
func ExecAllCap(reason string) (c app.SubjectFilter) {
	return app.SubjectFilter{Pattern: SubjectExecAll, Direction: app.CapDirectionBoth, Reason: reason}
}
