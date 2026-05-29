//go:build llm_generated_opus47

package supervisor

// InflightStateE classifies an in-flight row. Promoted to abandoned by
// the heartbeat watchdog when the producer goes silent past
// HeartbeatThresholdMs; promoted to cancelling on OnCancel before the
// producer's terminal verb arrives.
//
// The wire shape (task.InflightSnapshotEntry.State) is a string —
// "running" | "cancelling" | "abandoned" — so consumers do not need
// to import this package to decode a snapshot reply.
type InflightStateE uint8

const (
	InflightStateUnspecified InflightStateE = 0
	InflightStateRunning     InflightStateE = 1
	InflightStateCancelling  InflightStateE = 2
	InflightStateAbandoned   InflightStateE = 3
)

func (inst InflightStateE) String() (s string) {
	switch inst {
	case InflightStateRunning:
		s = "running"
	case InflightStateCancelling:
		s = "cancelling"
	case InflightStateAbandoned:
		s = "abandoned"
	default:
		s = "unspecified"
	}
	return
}
