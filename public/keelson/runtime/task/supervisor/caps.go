//go:build llm_generated_opus47

package supervisor

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
)

// Caps returns the SubjectFilter set the supervisor's bus client needs:
//
//   - task.> Sub                  — observe lifecycle for audit + inflight
//   - task.list.inflight Sub      — receive list-inflight requests
//   - _INBOX.> Pub                — reply on consumer inboxes
//
// Same shape as persist/fsbroker — services need _INBOX.> publish to
// reply, and Sub on their own subject space.
func Caps() (caps []app.SubjectFilter) {
	caps = []app.SubjectFilter{
		{Pattern: task.PatternAll, Direction: app.CapDirectionSub, Reason: "supervisor: observe lifecycle"},
		{Pattern: task.SubjectListInflight, Direction: app.CapDirectionSub, Reason: "supervisor: serve list-inflight requests"},
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionPub, Reason: "supervisor: reply to inboxes"},
	}
	return
}

// RequesterCaps returns the SubjectFilter set a consumer needs to query
// the supervisor's in-flight snapshot via Request. The reply inbox
// subscribe is bypassed by the inprocbus client (see permission.go) so
// only the publish cap is required.
func RequesterCaps() (caps []app.SubjectFilter) {
	caps = []app.SubjectFilter{
		{Pattern: task.SubjectListInflight, Direction: app.CapDirectionPub, Reason: "supervisor: query in-flight snapshot"},
	}
	return
}
